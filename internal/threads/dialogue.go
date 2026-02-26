/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package threads

import (
	"context"
	"fmt"
	"time"

	"github.com/mikeb26/octium/internal/am"
	"github.com/mikeb26/octium/internal/llmclient"
	"github.com/mikeb26/octium/internal/prompts"
	"github.com/mikeb26/octium/internal/types"
	"github.com/mikeb26/octium/internal/types/aiclient"
)

// setRunning transitions the thread to ThreadStateRunning.
//
// NOTE: Callers that need a stable reference for the lifetime of a request
// should call this once and hold on to the returned pointer
func (thr *thread) setRunning(ctx context.Context,
	ictx *types.InternalContext) (*thread, error) {
	thr.mu.Lock()
	defer thr.mu.Unlock()

	if thr.state != ThreadStateIdle {
		return nil, fmt.Errorf("cannot set non-idle thread to running state:%v",
			thr.state)
	}

	thr.persisted.InvocationCount++
	thr.state = ThreadStateRunning

	// Create the async approver and LLM client per-thread.
	if thr.needLLMReinit {
		if thr.asyncApprover != nil {
			thr.asyncApprover.Close()
		}
		thr.asyncApprover = nil
		thr.llmClient = nil
		thr.needLLMReinit = false
	}
	if thr.asyncApprover == nil {
		thr.asyncApprover = NewAsyncApprover(ictx.ASettings.BaseApprover)
	}

	effort := ictx.LlmSettings.ReasoningEffort
	if effort == "" {
		effort = types.ReasoningEffortMedium
	}
	if thr.llmClient == nil {
		approver := am.NewPolicyStoreApprover(thr.asyncApprover,
			ictx.ASettings.PolicyStore)
		thr.llmClient = llmclient.NewEINOClient(ctx, ictx, approver, 0)
	}
	// Ensure per-thread clients reflect the current runtime reasoning effort
	// (which can be changed via Preferences).
	thr.llmClient.SetReasoning(effort)

	return thr, nil
}

func finalizeChatOnce(thread *thread,
	fullDialogue []*types.ThreadMessage) error {

	thread.mu.Lock()
	defer thread.mu.Unlock()

	thread.persisted.Dialogue = fullDialogue
	thread.persisted.ModTime = time.Now()
	thread.state = ThreadStateIdle
	thread.runState = nil

	if err := thread.save(); err != nil {
		return err
	}

	return nil
}

// abortChatOnce resets a thread back to idle after a failed in-flight chat.
//
// Unlike finalizeChatOnce, this does not persist any dialogue changes.
// It is intended to be called by the async worker goroutine when it
// encounters an error (including cancellations).
func abortChatOnce(thread *thread) {
	thread.mu.Lock()
	defer thread.mu.Unlock()

	thread.state = ThreadStateIdle
	thread.runState = nil
}

// summarizeDialogue summarizes the entire chat history in order to reduce LLM
// token costs and refocus the context window.
func summarizeDialogue(ctx context.Context, llmClient aiclient.AIClient,
	sysMsg *types.ThreadMessage,
	dialogue []*types.ThreadMessage) ([]*types.ThreadMessage, error) {

	summaryDialogue := []*types.ThreadMessage{
		sysMsg,
	}

	msg := &types.ThreadMessage{
		Role:    types.LlmRoleSystem,
		Content: prompts.SummarizeMsg,
	}
	dialogue = append(dialogue, msg)

	msg, err := llmClient.CreateChatCompletion(ctx, dialogue)
	if err != nil {
		return summaryDialogue, err
	}

	summaryDialogue = append(summaryDialogue, msg)

	return summaryDialogue, nil
}
