/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package llmclient

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/gemini"
	openaigo "github.com/cloudwego/eino-ext/components/model/openai-go"
	"github.com/cloudwego/eino-ext/components/model/openrouter"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
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

	approver     am.Approver
	baseTools    []tool.BaseTool
	// modelFactory rebuilds models whose reasoning effort is set only at model
	// construction time. Adaptive Claude thinking sends output_config.effort in
	// the model configuration, so SetReasoning must replace its agent rather
	// than relying solely on a per-request option.
	modelFactory func(types.ReasoningEffort) model.ToolCallingChatModel

	agentMu sync.RWMutex

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

	chatModel, err := openaigo.NewChatModel(ctx, &openaigo.Config{Model: model, APIKey: apiKey})
	if err != nil {
		panic(err)
	}

	return newEINOClient(ctx, vendor, chatModel, approver, apiKey, model, depth,
		enableAuditLog, auditLogPath)
}

func newAnthropicEINOClient(ctx context.Context, vendor string,
	approver am.Approver, apiKey string, modelName string,
	depth int,
	enableAuditLog bool, auditLogPath string) aiclient.AIClient {

	chatModel, err := newAnthropicChatModel(ctx, apiKey, modelName,
		types.ReasoningEffortMedium)
	if err != nil {
		panic(err)
	}

	client := newEINOClient(ctx, vendor, chatModel, approver, apiKey, modelName, depth,
		enableAuditLog, auditLogPath).(*EINOAIClient)
	if usesAdaptiveClaudeThinking(modelName) {
		client.modelFactory = func(effort types.ReasoningEffort) model.ToolCallingChatModel {
			chatModel, err := newAnthropicChatModel(ctx, apiKey, modelName, effort)
			if err != nil {
				panic(err)
			}
			return chatModel
		}
	}

	return client
}

func newAnthropicChatModel(ctx context.Context, apiKey string, modelName string,
	effort types.ReasoningEffort) (model.ToolCallingChatModel, error) {

	config := &claude.Config{
		Model:  modelName,
		APIKey: apiKey,
		// currently hardcode max tokens to 64k; see
		// https://platform.claude.com/docs/en/api/go/messages/create
		// https://platform.claude.com/docs/en/about-claude/models/overview
		MaxTokens: 64000,
	}
	if usesAdaptiveClaudeThinking(modelName) {
		config.AdditionalRequestFields = map[string]any{
			"output_config": map[string]any{
				"effort": effort.String(),
			},
		}
	}

	return claude.NewChatModel(ctx, config)
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
	client, err := newReactAgent(ctx, chatModel, baseTools)
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
		baseTools:       baseTools,
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
	//	if depth <= internal.MaxDepth {
	//		tools = append(tools, newPromptRunTool(ctx, vendor, approver, apiKey,
	//			model, depth))
	//	}

	return tools
}

func (client *EINOAIClient) SetReasoning(
	reasoningEffort types.ReasoningEffort) {
	client.agentMu.Lock()
	defer client.agentMu.Unlock()

	client.reasoningEffort = reasoningEffort
	if client.modelFactory == nil {
		return
	}

	chatModel := client.modelFactory(reasoningEffort)
	agent, err := newReactAgent(context.Background(), chatModel, client.baseTools)
	if err != nil {
		panic(err)
	}
	client.reactAgent = agent
}

func (client *EINOAIClient) reasoningModelOption() model.Option {
	switch client.vendor {
	case "openrouter":
		return openrouter.WithReasoning(&openrouter.Reasoning{
			Effort: openRouterEffortFromReasoningEffort(client.reasoningEffort),
		})
	case "openai":
		return openaigo.WithReasoning(&openaigo.Reasoning{
			Effort:  openAIGoEffortFromReasoningEffort(client.reasoningEffort),
			Summary: openaigo.ReasoningSummaryAuto,
		})
	case "google":
		return gemini.WithThinkingConfig(&genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingLevel:   geminiThinkingLevelFromReasoningEffort(client.reasoningEffort),
		})
	case "anthropic":
		if usesAdaptiveClaudeThinking(client.model) {
			return claude.WithThinkingConfig(&anthropic.ThinkingConfigParamUnion{
				OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
			})
		}
		return claude.WithThinking(&claude.Thinking{
			Enable:       true,
			BudgetTokens: claudeBudgetTokensFromReasoningEffort(client.reasoningEffort),
		})
	default:
		return model.Option{}
	}
}

func newReactAgent(ctx context.Context, chatModel model.ToolCallingChatModel,
	tools []tool.BaseTool) (*react.Agent, error) {

	return react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: chatModel,
		MaxStep:          1000,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: tools,
		},
	})
}

func usesAdaptiveClaudeThinking(modelName string) bool {
	parts := strings.Split(modelName, "-")
	for i, part := range parts {
		majorMinor := strings.SplitN(part, ".", 2)
		major, err := strconv.Atoi(majorMinor[0])
		if err != nil {
			continue
		}
		if major > 4 {
			return true
		}
		if major < 4 {
			return false
		}
		minorPart := ""
		if len(majorMinor) == 2 {
			minorPart = majorMinor[1]
		} else if i+1 < len(parts) {
			minorPart = parts[i+1]
		}
		minor, err := strconv.Atoi(minorPart)
		return err == nil && minor >= 7
	}
	return false
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

func openAIGoEffortFromReasoningEffort(level types.ReasoningEffort) openaigo.ReasoningEffort {
	switch level {
	case types.ReasoningEffortLow:
		return openaigo.ReasoningEffortLow
	case types.ReasoningEffortHigh:
		return openaigo.ReasoningEffortHigh
	default:
		return openaigo.ReasoningEffortMedium
	}
}

func (client *EINOAIClient) CreateChatCompletion(ctx context.Context,
	dialogueIn []*types.ThreadMessage) (*types.ThreadMessage, error) {

	dialogue := make([]*schema.Message, len(dialogueIn))
	for ii, msg := range dialogueIn {
		dialogue[ii] = (*schema.Message)(msg)
	}

	client.agentMu.RLock()
	defer client.agentMu.RUnlock()

	modelOpt := client.reasoningModelOption()
	composeOpt := compose.WithChatModelOption(modelOpt)
	agentOpt := agent.WithComposeOptions(composeOpt)

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

	client.agentMu.RLock()
	defer client.agentMu.RUnlock()

	modelOpt := client.reasoningModelOption()
	composeOpt := compose.WithChatModelOption(modelOpt)
	agentOpt := agent.WithComposeOptions(composeOpt)

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
