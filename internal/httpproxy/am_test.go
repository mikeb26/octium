/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package httpproxy

import (
	"net/http"
	"testing"

	"github.com/mikeb26/gptcli/internal/am"
)

func TestHasAllApprovalActions(t *testing.T) {
	if !hasAllApprovalActions(nil, nil) {
		t.Fatalf("expected empty need to be satisfied")
	}

	have := []am.ApprovalAction{am.ApprovalActionRead}
	need := []am.ApprovalAction{am.ApprovalActionRead}
	if !hasAllApprovalActions(have, need) {
		t.Fatalf("expected have=%v to satisfy need=%v", have, need)
	}

	need = []am.ApprovalAction{am.ApprovalActionRead, am.ApprovalActionWrite}
	if hasAllApprovalActions(have, need) {
		t.Fatalf("expected have=%v to not satisfy need=%v", have, need)
	}
}

func TestRequestTarget_CONNECT(t *testing.T) {
	r, _ := http.NewRequest(http.MethodConnect, "https://example.com", nil)
	// CONNECT uses r.Host, not the URL's Host.
	r.Host = "example.com"

	host, port, scheme, ok := requestTarget(r)
	if !ok {
		t.Fatalf("expected ok")
	}
	if host != "example.com" || port != "443" || scheme != "https" {
		t.Fatalf("unexpected target: host=%q port=%q scheme=%q", host, port, scheme)
	}

	r.Host = "example.com:8443"
	host, port, scheme, ok = requestTarget(r)
	if !ok {
		t.Fatalf("expected ok")
	}
	if host != "example.com" || port != "8443" || scheme != "https" {
		t.Fatalf("unexpected target: host=%q port=%q scheme=%q", host, port, scheme)
	}
}

func TestRequestTarget_HTTPAbsoluteAndOriginForm(t *testing.T) {
	// absolute-form
	rAbs, _ := http.NewRequest(http.MethodGet, "http://example.com:8080/p", nil)
	host, port, scheme, ok := requestTarget(rAbs)
	if !ok {
		t.Fatalf("expected ok")
	}
	if host != "example.com" || port != "8080" || scheme != "http" {
		t.Fatalf("unexpected target: host=%q port=%q scheme=%q", host, port, scheme)
	}

	// origin-form (no scheme/host in URL)
	rOrig, _ := http.NewRequest(http.MethodGet, "/p", nil)
	rOrig.Host = "example.com"
	host, port, scheme, ok = requestTarget(rOrig)
	if !ok {
		t.Fatalf("expected ok")
	}
	if host != "example.com" || port != "80" || scheme != "http" {
		t.Fatalf("unexpected target: host=%q port=%q scheme=%q", host, port, scheme)
	}
}

func TestBuildPolicyIDForRequest_MethodMapping(t *testing.T) {
	rGet, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	policyID, actions, ok := buildPolicyIDForRequest(rGet)
	if !ok {
		t.Fatalf("expected ok")
	}
	if policyID != "tools:web:domain:http://example.com:80" {
		t.Fatalf("unexpected policyID: %q", policyID)
	}
	if len(actions) != 1 || actions[0] != am.ApprovalActionRead {
		t.Fatalf("unexpected actions: %v", actions)
	}

	rPost, _ := http.NewRequest(http.MethodPost, "http://example.com/", nil)
	_, actions, ok = buildPolicyIDForRequest(rPost)
	if !ok {
		t.Fatalf("expected ok")
	}
	if len(actions) != 2 || actions[0] != am.ApprovalActionWrite || actions[1] != am.ApprovalActionRead {
		t.Fatalf("unexpected actions for POST: %v", actions)
	}

	// Empty/whitespace method should default to GET.
	rEmpty, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	rEmpty.Method = "   "
	_, actions, ok = buildPolicyIDForRequest(rEmpty)
	if !ok {
		t.Fatalf("expected ok")
	}
	if len(actions) != 1 || actions[0] != am.ApprovalActionRead {
		t.Fatalf("unexpected actions for empty method: %v", actions)
	}
}
