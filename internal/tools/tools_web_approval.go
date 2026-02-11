/* Copyright © 2025 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package tools

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/mikeb26/gptcli/internal"
	"github.com/mikeb26/gptcli/internal/am"
	"github.com/mikeb26/gptcli/internal/types"
)

// buildWebApprovalRequest is a shared helper for web-oriented tools
// (e.g., url_retrieve and url_render) that need consistent approval
// behavior with per-URL and per-domain caching.
func buildWebApprovalRequest(t types.Tool, arg any, rawURL, method string) am.ApprovalRequest {
	// Parse the URL to extract a stable origin/domain component for
	// domain-scoped policies. If parsing fails, fall back to the
	// default approval behavior to avoid mis-caching.
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return DefaultApprovalRequest(t, arg)
	}

	port := parsed.Port()
	if port == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}

	m := strings.ToUpper(strings.TrimSpace(method))
	if m == "" {
		m = "GET"
	}

	domainKey := fmt.Sprintf("%s://%s", parsed.Scheme,
		net.JoinHostPort(parsed.Hostname(), port))
	domainPolicyID := am.ApprovalPolicyID(am.ApprovalSubsysTools,
		am.ApprovalGroupWeb, am.ApprovalTargetDomain, domainKey)

	promptBuilder := &strings.Builder{}
	promptBuilder.WriteString(fmt.Sprintf("%s would like to %v(%v): %v. Allow?",
		internal.CliToolName, t.GetOp(), m, rawURL))

	choices := []am.ApprovalChoice{
		{
			Key:   "y",
			Label: "Yes, this time only",
			Scope: am.ApprovalScopeOnce,
		},
	}

	choices = append(choices,
		am.ApprovalChoice{
			Key: "dw",
			Label: fmt.Sprintf("Yes, and allow all future access for %v",
				domainKey),
			Scope:    am.ApprovalScopeTarget,
			PolicyID: domainPolicyID,
			Actions: []am.ApprovalAction{am.ApprovalActionWrite,
				am.ApprovalActionRead},
		},
	)

	choices = append(choices, am.ApprovalChoice{
		Key:   "n",
		Label: "No",
		Scope: am.ApprovalScopeDeny,
	})

	required := []am.ApprovalAction{am.ApprovalActionRead, am.ApprovalActionWrite}

	return am.ApprovalRequest{
		Prompt:          promptBuilder.String(),
		RequiredActions: required,
		Choices:         choices,
	}
}
