/* Copyright © 2024-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aiclient

import (
	"context"

	"github.com/mikeb26/octium/internal/types"
)

// NOTE: gomock/mockgen does not yet fully understand Go generics syntax such
// as *schema.StreamReader[*ThreadMessage], so we no longer auto-generate this
// mock via go:generate. The mock implementation in openai_client_mock.go is
// maintained by hand.
//
//go:generate echo "skipping gomock generation for AIClient; using hand-maintained mock in openai_client_mock.go"
type AIClient interface {
	CreateChatCompletion(context.Context, []*types.ThreadMessage) (*types.ThreadMessage, error)
	StreamChatCompletion(context.Context, []*types.ThreadMessage) (*types.StreamResult, error)
	SetReasoning(types.ReasoningEffort)
	SubscribeProgress(string) chan types.ProgressEvent
	UnsubscribeProgress(chan types.ProgressEvent, string)
}
