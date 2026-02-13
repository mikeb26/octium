/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package tools

import (
	"context"
	"fmt"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/am"
	"github.com/mikeb26/octium/internal/types"
)

// GetUserApproval is a helper that enforces the RequiresUserApproval contract
// and delegates the actual interaction to the provided ToolApprovalUI.
func GetUserApproval(ctx context.Context, approver am.Approver,
	t types.Tool, arg any) error {

	if !t.RequiresUserApproval() {
		return nil
	}

	var req am.ApprovalRequest
	if ca, ok := t.(ToolWithCustomApproval); ok {
		r, rerr := ca.BuildApprovalRequest(ctx, arg)
		if rerr != nil {
			return rerr
		}
		req = r
	} else {
		req = DefaultApprovalRequest(t, arg)
	}
	dec, err := approver.AskApproval(ctx, req)
	if err != nil {
		return err
	}
	if !dec.Allowed {
		return fmt.Errorf("The user denied approval for us to run %v(%v); you(the AI agent) should provide justification to the octium user for why we need to invoke it.",
			t.GetOp(), arg)
	}

	return nil
}

// ToolWithCustomApproval can be implemented by tools that want to
// customize their approval prompt and options.
type ToolWithCustomApproval interface {
	BuildApprovalRequest(ctx context.Context, arg any) (am.ApprovalRequest, error)
}

// DefaultApprovalRequest builds the legacy yes/no style approval
// request used by tools that do not customize their approvals.
func DefaultApprovalRequest(t types.Tool, arg any) am.ApprovalRequest {
	prompt := fmt.Sprintf("%s would like to '%v'('%v')\nallow?", internal.CliToolName, t.GetOp(), arg)
	choices := []am.ApprovalChoice{
		{
			Key:   "y",
			Label: "yes",
			Scope: am.ApprovalScopeOnce,
		},
		{
			Key:   "n",
			Label: "no",
			Scope: am.ApprovalScopeDeny,
		},
	}

	return am.ApprovalRequest{
		Prompt:  prompt,
		Choices: choices,
	}
}
