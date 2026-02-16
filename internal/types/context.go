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

type ApproveSettings struct {
	BaseApprover        am.Approver
	PolicyStore         am.ApprovalPolicyStore
	RunCmdNeedsApproval bool
}

type LlmSettings struct {
	Vendor          string
	Model           string
	ApiKey          string
	AuditLogPath    string
	ReasoningEffort laclopenai.ReasoningEffortLevel
}

type InternalContext struct {
	LlmSettings LlmSettings
	ASettings   ApproveSettings

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

type workspacePwdKey struct{}

// WithWorkspacePwd returns a context with a workspace "present working
// directory" attached. This is used by lower layers (e.g. OS command tools)
// to run within the user's workspace sandbox.
func WithWorkspacePwd(ctx context.Context, pwd string) context.Context {
	return context.WithValue(ctx, workspacePwdKey{}, pwd)
}

// GetWorkspacePwd retrieves the workspace pwd from a context, if any.
func GetWorkspacePwd(ctx context.Context) (string, bool) {
	if v := ctx.Value(workspacePwdKey{}); v != nil {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}

func NewIctx(vendor, model, apiKey, auditPath string, approver am.Approver,
	policyStore am.ApprovalPolicyStore, proxy *httpproxy.HttpProxy,
	afs fsatomic.AtomicFS) *InternalContext {

	return &InternalContext{
		LlmSettings: LlmSettings{
			Vendor:          vendor,
			Model:           model,
			ApiKey:          apiKey,
			AuditLogPath:    auditPath,
			ReasoningEffort: laclopenai.ReasoningEffortLevelMedium,
		},
		ASettings: ApproveSettings{
			BaseApprover:        approver,
			PolicyStore:         policyStore,
			RunCmdNeedsApproval: false,
		},
		HttpProxy: proxy,
		Afs:       afs,
	}
}
