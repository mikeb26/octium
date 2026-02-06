/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikeb26/gptcli/internal/am"
)

func Test_RetrieveUrlTool_Invoke_RequiresExactlyOneOfTruncateSizeOrRespBodyFilename(t *testing.T) {
	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true}}
	tool := RetrieveUrlTool{approver: fa}

	resp, err := tool.Invoke(context.Background(), &RetrieveUrlReq{Url: "http://example.invalid"})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error == "" || !strings.Contains(resp.Error, "must be set") {
		t.Fatalf("expected validation error; got %q", resp.Error)
	}
}

func Test_RetrieveUrlTool_Invoke_TruncateSizeAndRespBodyFilenameAreMutuallyExclusive(t *testing.T) {
	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true}}
	tool := RetrieveUrlTool{approver: fa}

	resp, err := tool.Invoke(context.Background(), &RetrieveUrlReq{
		Url:              "http://example.invalid",
		TruncateSize:     10,
		RespBodyFilename: "out.txt",
	})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error == "" || !strings.Contains(resp.Error, "mutually exclusive") {
		t.Fatalf("expected mutual-exclusion error; got %q", resp.Error)
	}
}

func Test_RetrieveUrlTool_Invoke_DeniedApproval_DoesNotHitNetwork(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: false}}
	tool := RetrieveUrlTool{approver: fa}

	resp, err := tool.Invoke(context.Background(), &RetrieveUrlReq{Url: srv.URL, TruncateSize: 10})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error == "" || !strings.Contains(resp.Error, "denied approval") {
		t.Fatalf("expected approval-denied error; got %q", resp.Error)
	}
	if hits != 0 {
		t.Fatalf("expected no network request when approval denied; hits=%d", hits)
	}
}

func Test_RetrieveUrlTool_Invoke_TruncatesBodyAndSetsMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected method GET; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true, Choice: am.ApprovalChoice{Key: "y", Scope: am.ApprovalScopeOnce}}}
	tool := RetrieveUrlTool{approver: fa}

	resp, err := tool.Invoke(context.Background(), &RetrieveUrlReq{Url: srv.URL, TruncateSize: 5})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("expected resp.Error empty; got %q", resp.Error)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected StatusCode=200; got %d", resp.StatusCode)
	}
	if resp.Mode != "raw" {
		t.Fatalf("expected Mode=raw; got %q", resp.Mode)
	}

	if resp.Body.UntruncatedContentLen != len("hello world") {
		t.Fatalf("unexpected UntruncatedContentLen; want %d got %d", len("hello world"), resp.Body.UntruncatedContentLen)
	}
	if !resp.Body.WasTruncated {
		t.Fatalf("expected WasTruncated=true")
	}
	if resp.Body.Content != "hello" {
		t.Fatalf("unexpected truncated body; want %q got %q", "hello", resp.Body.Content)
	}
}

func Test_RetrieveUrlTool_Invoke_RespBodyFilename_WritesFullBodyAndReturnsEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true, Choice: am.ApprovalChoice{Key: "y", Scope: am.ApprovalScopeOnce}}}
	tool := RetrieveUrlTool{approver: fa}

	out := filepath.Join(t.TempDir(), "sub", "out.txt")
	resp, err := tool.Invoke(context.Background(), &RetrieveUrlReq{Url: srv.URL, RespBodyFilename: out})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("expected resp.Error empty; got %q", resp.Error)
	}
	if resp.BodyFilename != out {
		t.Fatalf("expected BodyFilename to echo output path; want %q got %q", out, resp.BodyFilename)
	}

	if resp.Body.Content != "" {
		t.Fatalf("expected response body content to be empty when writing to file; got %q", resp.Body.Content)
	}
	if resp.Body.WasTruncated {
		t.Fatalf("expected WasTruncated=false when writing to file")
	}
	if resp.Body.UntruncatedContentLen != len("hello world") {
		t.Fatalf("unexpected UntruncatedContentLen; want %d got %d", len("hello world"), resp.Body.UntruncatedContentLen)
	}

	b, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatalf("expected output file to exist; got %v", rerr)
	}
	if string(b) != "hello world" {
		t.Fatalf("unexpected file content; want %q got %q", "hello world", string(b))
	}
}

func Test_RetrieveUrlTool_Invoke_POST_SendsBodyAndHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected method POST; got %s", r.Method)
		}
		if got := r.Header.Get("X-Test"); got != "1" {
			t.Fatalf("expected X-Test header=1; got %q", got)
		}
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if string(b) != "abc" {
			t.Fatalf("expected request body %q; got %q", "abc", string(b))
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true, Choice: am.ApprovalChoice{Key: "y", Scope: am.ApprovalScopeOnce}}}
	tool := RetrieveUrlTool{approver: fa}

	resp, err := tool.Invoke(context.Background(), &RetrieveUrlReq{
		Url:          srv.URL,
		Method:       "POST",
		Body:         "abc",
		TruncateSize: 10,
		Headers: []RetrieveUrlRequestHeader{
			{Key: "X-Test", Value: "1"},
		},
	})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("expected resp.Error empty; got %q", resp.Error)
	}
	if resp.Body.Content != "ok" {
		t.Fatalf("unexpected response body; want %q got %q", "ok", resp.Body.Content)
	}
}

func Test_normalizeHTTPRequestMethod_DefaultsToGET(t *testing.T) {
	if got := normalizeHTTPRequestMethod(""); got != "GET" {
		t.Fatalf("expected GET; got %q", got)
	}
	if got := normalizeHTTPRequestMethod("  "); got != "GET" {
		t.Fatalf("expected GET; got %q", got)
	}
	if got := normalizeHTTPRequestMethod("post"); got != "POST" {
		t.Fatalf("expected POST; got %q", got)
	}
}

func Test_shouldAutoRenderRetrievedBody_JavaScriptContentType(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/javascript")
	if !shouldAutoRenderRetrievedBody(h, "console.log(1);") {
		t.Fatalf("expected shouldAutoRenderRetrievedBody=true for javascript content-type")
	}
}

func Test_shouldAutoRenderRetrievedBody_PlainTextDoesNotAutoRender(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "text/plain")
	if shouldAutoRenderRetrievedBody(h, "hello") {
		t.Fatalf("expected shouldAutoRenderRetrievedBody=false for plain text")
	}
}
