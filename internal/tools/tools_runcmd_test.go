/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package tools

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/am"
	"github.com/mikeb26/octium/internal/httpproxy"
	"github.com/mikeb26/octium/internal/types"
)

type fakeApprover struct {
	decision am.ApprovalDecision
	err      error
	lastReq  am.ApprovalRequest
}

func (f *fakeApprover) AskApproval(ctx context.Context, req am.ApprovalRequest) (am.ApprovalDecision, error) {
	f.lastReq = req
	if f.err != nil {
		return am.ApprovalDecision{}, f.err
	}
	return f.decision, nil
}

func Test_buildCommandInvocationKey_DeterministicAndSensitiveToArgs(t *testing.T) {
	k1 := buildCommandInvocationKey("go", []string{"test", "./..."})
	k2 := buildCommandInvocationKey("go", []string{"test", "./..."})
	if k1 != k2 {
		t.Fatalf("expected deterministic key; got %q vs %q", k1, k2)
	}
	if !strings.HasPrefix(k1, "go:") {
		t.Fatalf("expected key to have cmd prefix; got %q", k1)
	}

	k3 := buildCommandInvocationKey("go", []string{"test", "./pkg"})
	if k1 == k3 {
		t.Fatalf("expected different args to produce different keys; got %q", k1)
	}

	h := strings.TrimPrefix(k1, "go:")
	if len(h) != 16 {
		t.Fatalf("expected 8-byte hex hash (16 chars); got %q (len=%d)", h, len(h))
	}
}

func Test_setContent_TruncatesAndSetsMetadata(t *testing.T) {
	out := setContent("0123456789", 5)
	if out.UntruncatedContentLen != 10 {
		t.Fatalf("unexpected UntruncatedContentLen; want 10 got %d", out.UntruncatedContentLen)
	}
	if !out.WasTruncated {
		t.Fatalf("expected WasTruncated=true")
	}
	// NOTE: current implementation truncates to maxLen-1.
	if out.Content != "01234" {
		t.Fatalf("unexpected truncated content; want %q got %q", "01234", out.Content)
	}

	out2 := setContent("abc", 5)
	if out2.WasTruncated {
		t.Fatalf("expected WasTruncated=false")
	}
	if out2.Content != "abc" {
		t.Fatalf("unexpected content; want %q got %q", "abc", out2.Content)
	}
}

func Test_RunCommandTool_Invoke_ApproverError_SkipsExecution(t *testing.T) {
	fa := &fakeApprover{err: fmt.Errorf("approver failure")}
	tool := RunCommandTool{approver: fa}
	ctx := types.WithIctx(context.Background(), &types.InternalContext{ASettings: types.ApproveSettings{RunCmdNeedsApproval: true}})
	resp, err := tool.Invoke(ctx, &CmdRunReq{Cmd: os.Args[0], CmdArgs: []string{"-test.run=TestHelperProcess"}, TruncateSize: 1024})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error != "approver failure" {
		t.Fatalf("expected resp.Error to be approver error; got %q", resp.Error)
	}
}

func Test_RunCommandTool_Invoke_DeniedApproval_SkipsExecution(t *testing.T) {
	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: false}}
	tool := RunCommandTool{approver: fa}
	ctx := types.WithIctx(context.Background(), &types.InternalContext{ASettings: types.ApproveSettings{RunCmdNeedsApproval: true}})
	resp, err := tool.Invoke(ctx, &CmdRunReq{Cmd: os.Args[0], CmdArgs: []string{"-test.run=TestHelperProcess", "--", "stdout", "DENIED", "0"}, TruncateSize: 1024})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error == "" {
		t.Fatalf("expected resp.Error to be set when approval denied")
	}
	if resp.Stdout.Content != "" || resp.Stderr.Content != "" {
		t.Fatalf("expected empty stdout/stderr when approval denied; got stdout=%q stderr=%q", resp.Stdout.Content, resp.Stderr.Content)
	}

	if fa.lastReq.Prompt == "" {
		t.Fatalf("expected AskApproval to be called with a prompt")
	}
}

func Test_RunCommandTool_Invoke_ExecutesCommand_CapturesOutputAndExitError(t *testing.T) {
	ta := &fakeApprover{decision: am.ApprovalDecision{Allowed: true, Choice: am.ApprovalChoice{Key: "y", Scope: am.ApprovalScopeOnce}}}
	tool := RunCommandTool{approver: ta}

	// Configure a proxy in context and point the privileged wrapper to a fake
	// helper that just execs the requested command.
	proxy := httpproxy.New(am.NewMemoryApprovalPolicyStore())
	if err := proxy.ListenAndServe(); err != nil {
		t.Fatalf("proxy.ListenAndServe: %v", err)
	}
	ctx := types.WithIctx(context.Background(), &types.InternalContext{HttpProxy: proxy})

	internal.CliLibexecDir = t.TempDir()
	runAsPath := internal.CliRunAsScriptPath()
	if err := os.MkdirAll(internal.CliLibexecDir+"/"+internal.CliToolName, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(runAsPath, []byte("#!/bin/sh\n\n# Ignore wrapper args; just exec the requested command.\nif [ \"$1\" = \"--proxy-addr\" ]; then\n  shift 2\nfi\nif [ \"$1\" = \"--cwd\" ]; then\n  shift 2\nfi\nif [ \"$1\" = \"--\" ]; then\n  shift\nfi\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(runAs): %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", internal.CliLibexecDir+"/"+internal.CliToolName+":"+oldPath)
	sudoPath := internal.CliLibexecDir + "/" + internal.CliToolName + "/sudo"
	if err := os.WriteFile(sudoPath, []byte("#!/bin/sh\n\n# In tests, avoid real sudo; just exec the command.\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(sudo): %v", err)
	}

	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	resp, err := tool.Invoke(ctx, &CmdRunReq{
		Cmd:          os.Args[0],
		CmdArgs:      []string{"-test.run=TestHelperProcess", "--", "stdout", "stderr", "2"},
		TruncateSize: 10,
	})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}

	if resp.Error == "" || !strings.Contains(resp.Error, "exit status") {
		t.Fatalf("expected exit error in resp.Error; got %q", resp.Error)
	}
	if resp.Stdout.Content != "stdout\n" {
		t.Fatalf("unexpected stdout; got %q", resp.Stdout.Content)
	}
	if resp.Stderr.Content != "stderr\n" {
		t.Fatalf("unexpected stderr; got %q", resp.Stderr.Content)
	}
}

func Test_RunCommandTool_Invoke_TruncatesStdoutAndStderr(t *testing.T) {
	ta := &fakeApprover{decision: am.ApprovalDecision{Allowed: true, Choice: am.ApprovalChoice{Key: "y", Scope: am.ApprovalScopeOnce}}}
	tool := RunCommandTool{approver: ta}

	proxy := httpproxy.New(am.NewMemoryApprovalPolicyStore())
	if err := proxy.ListenAndServe(); err != nil {
		t.Fatalf("proxy.ListenAndServe: %v", err)
	}
	ctx := types.WithIctx(context.Background(), &types.InternalContext{HttpProxy: proxy})

	internal.CliLibexecDir = t.TempDir()
	runAsPath := internal.CliRunAsScriptPath()
	if err := os.MkdirAll(internal.CliLibexecDir+"/"+internal.CliToolName, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(runAsPath, []byte("#!/bin/sh\n\nif [ \"$1\" = \"--proxy-addr\" ]; then\n  shift 2\nfi\nif [ \"$1\" = \"--cwd\" ]; then\n  shift 2\nfi\nif [ \"$1\" = \"--\" ]; then\n  shift\nfi\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(runAs): %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", internal.CliLibexecDir+"/"+internal.CliToolName+":"+oldPath)
	sudoPath := internal.CliLibexecDir + "/" + internal.CliToolName + "/sudo"
	if err := os.WriteFile(sudoPath, []byte("#!/bin/sh\n\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(sudo): %v", err)
	}

	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	resp, err := tool.Invoke(ctx, &CmdRunReq{
		Cmd:          os.Args[0],
		CmdArgs:      []string{"-test.run=TestHelperProcess", "--", "0123456789", "abcdefghij", "0"},
		TruncateSize: 6,
	})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("expected resp.Error empty for exit 0; got %q", resp.Error)
	}

	if !resp.Stdout.WasTruncated || resp.Stdout.Content != "012345" {
		t.Fatalf("unexpected stdout truncation; trunc=%v content=%q", resp.Stdout.WasTruncated, resp.Stdout.Content)
	}
	if !resp.Stderr.WasTruncated || resp.Stderr.Content != "abcdef" {
		t.Fatalf("unexpected stderr truncation; trunc=%v content=%q", resp.Stderr.WasTruncated, resp.Stderr.Content)
	}
}

func Test_RunCommandTool_Invoke_ProxyNotConfigured_ReturnsErrorAndSkipsExecution(t *testing.T) {
	ta := &fakeApprover{decision: am.ApprovalDecision{Allowed: true}}
	tool := RunCommandTool{approver: ta}
	ctx := types.WithIctx(context.Background(), &types.InternalContext{})
	resp, err := tool.Invoke(ctx, &CmdRunReq{Cmd: os.Args[0], CmdArgs: []string{"-test.run=TestHelperProcess"}})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error == "" || !strings.Contains(resp.Error, "no proxy") {
		t.Fatalf("expected proxy error; got %q", resp.Error)
	}
}

// TestHelperProcess is a standard helper-process pattern. It is invoked
// by executing the test binary itself and is gated by the
// GO_WANT_HELPER_PROCESS environment variable.
//
// Args after "--" are:
//   - stdout string
//   - stderr string
//   - exit code integer
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Do not run in parallel; this is a helper invoked via exec.

	args := os.Args
	idx := -1
	for i := range args {
		if args[i] == "--" {
			idx = i
			break
		}
	}
	if idx == -1 || len(args) < idx+4 {
		fmt.Fprintln(os.Stderr, "bad helper process args")
		os.Exit(2)
	}

	stdoutMsg := args[idx+1]
	stderrMsg := args[idx+2]
	exitCode, err := strconv.Atoi(args[idx+3])
	if err != nil {
		exitCode = 2
	}

	fmt.Fprintln(os.Stdout, stdoutMsg)
	fmt.Fprintln(os.Stderr, stderrMsg)
	os.Exit(exitCode)
}
