/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package types

import (
	"context"

	laclopenai "github.com/cloudwego/eino-ext/libs/acl/openai"
	"github.com/mikeb26/octium/internal/am"
	"github.com/mikeb26/octium/internal/fsatomic"
	"github.com/mikeb26/octium/internal/httpproxy"
)

type InternalContext struct {
	LlmVendor       string
	LlmModel        string
	LlmApiKey       string
	LlmAuditLogPath string
	// LlmReasoningEffort is a best-effort hint to the LLM client about how much
	// reasoning to apply. Expected values are "low", "medium", or "high".
	//
	// This is interpreted by llmclient.NewEINOClient and may be ignored by some
	// vendors/models.
	LlmReasoningEffort laclopenai.ReasoningEffortLevel
	LlmBaseApprover    am.Approver
	LlmPolicyStore     am.ApprovalPolicyStore

	HttpProxy *httpproxy.HttpProxy
	Afs       fsatomic.AtomicFS
}

type iCtxKey struct{}

// WithIctx returns a context with an InternalContext attached.
func WithIctx(ctx context.Context, ictx *InternalContext) context.Context {
	return context.WithValue(ctx, iCtxKey{}, ictx)
}

// GetIctx retrieves an InternalContext from a context, if any.
func GetIctx(ctx context.Context) (*InternalContext, bool) {
	if v := ctx.Value(iCtxKey{}); v != nil {
		if ictx, ok := v.(*InternalContext); ok && ictx != nil {
			return ictx, true
		}
	}
	return nil, false
}
