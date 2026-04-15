/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package llmclient

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/openrouter"
	laclopenai "github.com/cloudwego/eino-ext/libs/acl/openai"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/am"
	"github.com/mikeb26/octium/internal/tools"
	"github.com/mikeb26/octium/internal/types"
	"github.com/mikeb26/octium/internal/types/aiclient"
	"google.golang.org/genai"
)

type EINOAIClient struct {
	vendor          string
	model           string
	reactAgent      *react.Agent
	reasoningEffort types.ReasoningEffort
	auditHandler    callbacks.Handler
	statusHandlers  callbacks.Handler

	approver am.Approver

	subsMu sync.RWMutex
	subs   map[string][]chan types.ProgressEvent //index by invocationID

	// current holds the most recent progress event per invocation ID so that
	// late subscribers (e.g. UI subscribing after Stream() returns) can still
	// learn what is currently happening.
	currentMu sync.RWMutex
	current   map[string]types.ProgressEvent
}

// invocationIDKey is an unexported context key type used to store a per-
// invocation ID so that all audit log entries for a single originating call
// to CreateChatCompletion / StreamChatCompletion can be correlated.
type invocationIDKey struct{}

// GetInvocationID extracts the invocation ID from the context, if present.
func GetInvocationID(ctx context.Context) string {
	if v := ctx.Value(invocationIDKey{}); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func SetInvocationID(ctx context.Context, threadId string,
	invCount int) (context.Context, string) {

	invId := fmt.Sprintf("t%v.i%v", threadId, invCount)
	ctx = context.WithValue(ctx, invocationIDKey{}, invId)

	return ctx, invId
}

func NewEINOClient(ctx context.Context, ictx *types.InternalContext,
	approver am.Approver, depth int) aiclient.AIClient {

	vendor := ictx.LlmSettings.Vendor
	apiKey := ictx.LlmSettings.ApiKey
	modelName := ictx.LlmSettings.Model
	enableAuditLog := ictx.LlmSettings.AuditLogPath != ""
	auditLogPath := ictx.LlmSettings.AuditLogPath

	var client aiclient.AIClient
	switch vendor {
	case "openai":
		client = newOpenAIEINOClient(ctx, vendor, approver, apiKey, modelName, depth,
			enableAuditLog, auditLogPath)
	case "anthropic":
		client = newAnthropicEINOClient(ctx, vendor, approver, apiKey, modelName, depth,
			enableAuditLog, auditLogPath)
	case "google":
		client = newGoogleEINOClient(ctx, vendor, approver, apiKey, modelName, depth,
			enableAuditLog, auditLogPath)
	case "openrouter":
		client = newOpenRouterEINOClient(ctx, vendor, approver, apiKey, modelName, depth,
			enableAuditLog, auditLogPath)
	default:
		panic("unsupported vendor")
	}

	client.SetReasoning(ictx.LlmSettings.ReasoningEffort)

	return client
}

func newOpenAIEINOClient(ctx context.Context, vendor string,
	approver am.Approver, apiKey string, model string,
	depth int,
	enableAuditLog bool, auditLogPath string) aiclient.AIClient {

	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		Model:  model,
		APIKey: apiKey,
	})
	if err != nil {
		panic(err)
	}

	return newEINOClient(ctx, vendor, chatModel, approver, apiKey, model, depth,
		enableAuditLog, auditLogPath)
}

func newAnthropicEINOClient(ctx context.Context, vendor string,
	approver am.Approver, apiKey string, model string,
	depth int,
	enableAuditLog bool, auditLogPath string) aiclient.AIClient {

	chatModel, err := claude.NewChatModel(ctx, &claude.Config{
		Model:  model,
		APIKey: apiKey,
		// currently hardcode max tokens to 64k; see
		// https://platform.claude.com/docs/en/api/go/messages/create
		// https://platform.claude.com/docs/en/about-claude/models/overview
		MaxTokens: 64000,
	})
	if err != nil {
		panic(err)
	}

	return newEINOClient(ctx, vendor, chatModel, approver, apiKey, model, depth,
		enableAuditLog, auditLogPath)
}

func newGoogleEINOClient(ctx context.Context, vendor string,
	approver am.Approver, apiKey string, model string,
	depth int,
	enableAuditLog bool, auditLogPath string) aiclient.AIClient {

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		panic(err)
	}

	chatModel, err := gemini.NewChatModel(ctx, &gemini.Config{
		Model:  model,
		Client: client,
	})
	if err != nil {
		panic(err)
	}

	return newEINOClient(ctx, vendor, chatModel, approver, apiKey, model, depth,
		enableAuditLog, auditLogPath)
}

func newOpenRouterEINOClient(ctx context.Context, vendor string,
	approver am.Approver, apiKey string, model string,
	depth int,
	enableAuditLog bool, auditLogPath string) aiclient.AIClient {

	chatModel, err := openrouter.NewChatModel(ctx, &openrouter.Config{
		Model:  model,
		APIKey: apiKey,
	})
	if err != nil {
		panic(err)
	}

	return newEINOClient(ctx, vendor, chatModel, approver, apiKey, model, depth,
		enableAuditLog, auditLogPath)
}

func newEINOClient(ctx context.Context, vendor string, chatModel model.ToolCallingChatModel,
	approver am.Approver, apiKey string, model string,
	depth int, enableAuditLog bool, auditLogPath string) aiclient.AIClient {

	tools := defineTools(ctx, vendor, approver, apiKey, model, depth)
	baseTools := make([]tool.BaseTool, len(tools))
	for ii, _ := range tools {
		baseTools[ii] = tools[ii]
	}
	config := &react.AgentConfig{
		ToolCallingModel: chatModel,
		MaxStep:          1000,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: baseTools,
		},
	}

	client, err := react.NewAgent(ctx, config)
	if err != nil {
		panic(err)
	}

	var auditHandler callbacks.Handler
	if enableAuditLog {
		auditHandler, err = newAuditCallbacksHandler(auditLogPath)
		if err != nil {
			panic(err)
		}
	}

	clientOut := &EINOAIClient{
		vendor:          vendor,
		model:           model,
		reactAgent:      client,
		reasoningEffort: types.ReasoningEffortMedium,
		auditHandler:    auditHandler,
		approver:        approver,
		subs:            make(map[string][]chan types.ProgressEvent),
		current:         make(map[string]types.ProgressEvent),
	}
	clientOut.statusHandlers = newStatusCallbackHandlers(clientOut)
	return clientOut
}

func defineTools(ctx context.Context, vendor string, approver am.Approver,
	apiKey string, model string, depth int) []types.LlmTool {

	tools := []types.LlmTool{
		tools.NewRunCommandTool(approver),
		tools.NewCreateFileTool(approver),
		tools.NewAppendFileTool(approver),
		tools.NewFilePatchTool(approver),
		tools.NewReadFileTool(approver),
		tools.NewDeleteFileTool(approver),
		tools.NewRetrieveUrlTool(approver),
	}
	if depth <= internal.MaxDepth {
		tools = append(tools, newPromptRunTool(ctx, vendor, approver, apiKey,
			model, depth))
	}

	return tools
}

func (client *EINOAIClient) SetReasoning(
	reasoningEffort types.ReasoningEffort) {
	client.reasoningEffort = reasoningEffort
}

func isOpenAIGPT54Model(model string) bool {
	return strings.HasPrefix(model, "gpt-5.4")
}

func (client *EINOAIClient) reasoningModelOption() (modelOpt model.Option, include bool, err error) {
	switch client.vendor {
	case "openrouter":
		return openrouter.WithReasoning(&openrouter.Reasoning{
			Effort: openRouterEffortFromReasoningEffort(client.reasoningEffort),
		}), true, nil
	case "openai":
		// The EINO OpenAI bindings currently use the legacy completions interface
		// rather than the OpenAI Responses API, so setting reasoning effort for
		// gpt-5.4* models is not supported.
		if isOpenAIGPT54Model(client.model) {
			if client.reasoningEffort != types.ReasoningEffortMedium {
				return model.Option{}, false, fmt.Errorf(
					"%w: openai model %q does not support reasoning effort %q (only %q is supported due to EINO OpenAI legacy completions bindings)",
					ErrReasoningEffortNotSupported,
					client.model,
					client.reasoningEffort,
					types.ReasoningEffortMedium,
				)
			}
			// Medium is the default; don't set it explicitly.
			return model.Option{}, false, nil
		}
		return laclopenai.WithReasoningEffort(openAIEffortFromReasoningEffort(client.reasoningEffort)), true, nil
	case "google":
		return gemini.WithThinkingConfig(&genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingLevel:   geminiThinkingLevelFromReasoningEffort(client.reasoningEffort),
		}), true, nil
	case "anthropic":
		return claude.WithThinking(&claude.Thinking{
			Enable:       true,
			BudgetTokens: claudeBudgetTokensFromReasoningEffort(client.reasoningEffort),
		}), true, nil
	default:
		return model.Option{}, false, nil
	}
}

func geminiThinkingLevelFromReasoningEffort(level types.ReasoningEffort) genai.ThinkingLevel {
	switch level {
	case types.ReasoningEffortLow:
		return genai.ThinkingLevelLow
	case types.ReasoningEffortHigh:
		return genai.ThinkingLevelHigh
	default:
		return genai.ThinkingLevelMedium
	}
}

func claudeBudgetTokensFromReasoningEffort(level types.ReasoningEffort) int {
	switch level {
	case types.ReasoningEffortLow:
		return 1024
	case types.ReasoningEffortHigh:
		return 8192
	default:
		return 4096
	}
}

func openRouterEffortFromReasoningEffort(level types.ReasoningEffort) openrouter.Effort {
	switch level {
	case types.ReasoningEffortLow:
		return openrouter.EffortOfLow
	case types.ReasoningEffortHigh:
		return openrouter.EffortOfHigh
	default:
		return openrouter.EffortOfMedium
	}
}

func openAIEffortFromReasoningEffort(level types.ReasoningEffort) laclopenai.ReasoningEffortLevel {
	switch level {
	case types.ReasoningEffortLow:
		return laclopenai.ReasoningEffortLevelLow
	case types.ReasoningEffortHigh:
		return laclopenai.ReasoningEffortLevelHigh
	default:
		return laclopenai.ReasoningEffortLevelMedium
	}
}

func (client *EINOAIClient) CreateChatCompletion(ctx context.Context,
	dialogueIn []*types.ThreadMessage) (*types.ThreadMessage, error) {

	dialogue := make([]*schema.Message, len(dialogueIn))
	for ii, msg := range dialogueIn {
		dialogue[ii] = (*schema.Message)(msg)
	}

	modelOpt, includeModelOpt, err := client.reasoningModelOption()
	if err != nil {
		return nil, err
	}
	composeOpts := make([]compose.Option, 0)
	if includeModelOpt {
		composeOpts = append(composeOpts, compose.WithChatModelOption(modelOpt))
	}
	agentOpt := agent.WithComposeOptions(composeOpts...)

	// attach callbacks for model and tool invocations
	var cbComposeOpt compose.Option
	if client.auditHandler != nil {
		cbComposeOpt = compose.WithCallbacks(client.auditHandler,
			client.statusHandlers)
	} else {
		cbComposeOpt = compose.WithCallbacks(client.statusHandlers)
	}
	cbAgentOpt := agent.WithComposeOptions(cbComposeOpt)

	msg, err := client.reactAgent.Generate(ctx, dialogue, agentOpt,
		cbAgentOpt)
	return (*types.ThreadMessage)(msg), err
}

func (client *EINOAIClient) StreamChatCompletion(ctx context.Context,
	dialogueIn []*types.ThreadMessage) (*types.StreamResult, error) {

	// Ensure this invocation has a correlation ID for audit/progress callbacks.
	// If the caller already attached an ID to ctx, we will reuse it.
	invocationID := GetInvocationID(ctx)

	dialogue := make([]*schema.Message, len(dialogueIn))
	for ii, msg := range dialogueIn {
		dialogue[ii] = (*schema.Message)(msg)
	}

	modelOpt, includeModelOpt, err := client.reasoningModelOption()
	if err != nil {
		return nil, err
	}
	composeOpts := make([]compose.Option, 0)
	if includeModelOpt {
		composeOpts = append(composeOpts, compose.WithChatModelOption(modelOpt))
	}
	agentOpt := agent.WithComposeOptions(composeOpts...)

	// attach callbacks for model and tool invocations
	var cbComposeOpt compose.Option
	if client.auditHandler != nil {
		cbComposeOpt = compose.WithCallbacks(client.auditHandler,
			client.statusHandlers)
	} else {
		cbComposeOpt = compose.WithCallbacks(client.statusHandlers)
	}
	cbAgentOpt := agent.WithComposeOptions(cbComposeOpt)

	stream, err := client.reactAgent.Stream(ctx, dialogue, agentOpt, cbAgentOpt)
	if err != nil {
		return nil, err
	}

	convert := func(m *schema.Message) (*types.ThreadMessage, error) {
		if m == nil {
			return nil, fmt.Errorf("nil message in stream")
		}
		return (*types.ThreadMessage)(m), nil
	}

	streamOut := schema.StreamReaderWithConvert(stream, convert)
	return &types.StreamResult{
		InvocationID: invocationID,
		Stream:       streamOut,
	}, nil
}

// SubscribeProgress registers a subscriber for callback-driven progress events
// for the given invocation ID.
//
// The returned channel will receive events best-effort; if the receiver is too
// slow, events may be dropped. It is the caller's responsibility to call
// UnsubscribeProcess() when no longer required.
func (client *EINOAIClient) SubscribeProgress(
	invocationID string) chan types.ProgressEvent {

	ch := make(chan types.ProgressEvent, 64)
	if invocationID == "" {
		close(ch)
		return nil
	}

	client.subsMu.Lock()
	client.subs[invocationID] = append(client.subs[invocationID], ch)
	client.subsMu.Unlock()

	// Best-effort send the most recent known status for this invocation so the
	// caller doesn't miss early tool/model events that may have fired before the
	// subscription was established.
	client.currentMu.RLock()
	if ev, ok := client.current[invocationID]; ok {
		select {
		case ch <- ev:
		default:
		}
	}
	client.currentMu.RUnlock()

	return ch
}

// UnsubscribeProgress unregisters a subscriber from a previously subscribed
// invocationID
func (client *EINOAIClient) UnsubscribeProgress(ch chan types.ProgressEvent,
	invocationID string) {

	client.subsMu.Lock()
	defer client.subsMu.Unlock()

	subs := client.subs[invocationID]
	for i := range subs {
		if subs[i] == ch {
			subs = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
	if len(subs) == 0 {
		delete(client.subs, invocationID)
	} else {
		client.subs[invocationID] = subs
	}
}

func (client *EINOAIClient) publishProgress(invocationID string, ev types.ProgressEvent) {
	if invocationID == "" {
		return
	}

	// Store the latest event so late subscribers can catch up.
	client.currentMu.Lock()
	client.current[invocationID] = ev
	client.currentMu.Unlock()

	subs := make([]chan types.ProgressEvent, 0)

	// make a local copy of the set of subscribers so that new subscribers
	// don't race with iteration
	client.subsMu.RLock()
	subs = append(subs, client.subs[invocationID]...)
	client.subsMu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// drop if subscriber is slow
		}
	}
}
