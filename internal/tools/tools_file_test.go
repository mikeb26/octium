/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikeb26/octium/internal/am"
	"github.com/mikeb26/octium/internal/types"
)

func collectChoiceKeys(req am.ApprovalRequest) map[string]am.ApprovalChoice {
	m := make(map[string]am.ApprovalChoice, len(req.Choices))
	for _, c := range req.Choices {
		m[c.Key] = c
	}
	return m
}

func requireChoiceKey(t *testing.T, keys map[string]am.ApprovalChoice, key string) am.ApprovalChoice {
	t.Helper()
	c, ok := keys[key]
	if !ok {
		// Provide useful debug output.
		present := make([]string, 0, len(keys))
		for k := range keys {
			present = append(present, k)
		}
		t.Fatalf("expected choice key %q; present keys=%v", key, present)
	}
	return c
}

func Test_ReadFileTool_BuildApprovalRequest_NormalizesRelativePathsAndIncludesReadChoices(t *testing.T) {
	tmp := t.TempDir()
	ctx := types.WithWorkspacePwd(context.Background(), tmp)

	reqPath := "foo.txt" // relative
	tool := ReadFileTool{}
	ar, err := tool.BuildApprovalRequest(ctx, &ReadFileReq{Filename: reqPath})
	if err != nil {
		t.Fatalf("BuildApprovalRequest: %v", err)
	}
	if ar.Prompt == "" {
		t.Fatalf("expected non-empty prompt")
	}

	keys := collectChoiceKeys(ar)
	requireChoiceKey(t, keys, "y")
	requireChoiceKey(t, keys, "n")
	requireChoiceKey(t, keys, "fr")
	requireChoiceKey(t, keys, "fw")
	requireChoiceKey(t, keys, "dr")
	requireChoiceKey(t, keys, "dw")

	absFile := filepath.Clean(filepath.Join(tmp, reqPath))
	absDir := filepath.Dir(absFile)

	fr := requireChoiceKey(t, keys, "fr")
	if fr.PolicyID == "" || !strings.Contains(fr.PolicyID, absFile) {
		t.Fatalf("expected fr PolicyID to mention abs file path %q; got %q", absFile, fr.PolicyID)
	}

	dr := requireChoiceKey(t, keys, "dr")
	if dr.PolicyID == "" || !strings.Contains(dr.PolicyID, absDir) {
		t.Fatalf("expected dr PolicyID to mention abs dir path %q; got %q", absDir, dr.PolicyID)
	}
}

func Test_CreateFileTool_BuildApprovalRequest_DoesNotIncludeReadOnlyChoices(t *testing.T) {
	tmp := t.TempDir()
	ctx := types.WithWorkspacePwd(context.Background(), tmp)
	filename := filepath.Join(tmp, "bar.txt")

	tool := CreateFileTool{}
	ar, err := tool.BuildApprovalRequest(ctx, &CreateFileReq{Filename: filename, Content: "x"})
	if err != nil {
		t.Fatalf("BuildApprovalRequest: %v", err)
	}
	keys := collectChoiceKeys(ar)

	requireChoiceKey(t, keys, "y")
	requireChoiceKey(t, keys, "n")
	requireChoiceKey(t, keys, "fw")
	requireChoiceKey(t, keys, "dw")
	if _, ok := keys["fr"]; ok {
		t.Fatalf("did not expect fr choice for create (write-required)")
	}
	if _, ok := keys["dr"]; ok {
		t.Fatalf("did not expect dr choice for create (write-required)")
	}
}

func Test_CreateFileTool_Invoke_DeniedApproval_DoesNotWriteFile(t *testing.T) {
	tmp := t.TempDir()
	ctx := types.WithWorkspacePwd(context.Background(), tmp)
	filename := filepath.Join(tmp, "deny.txt")

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: false}}
	tool := CreateFileTool{approver: fa}

	resp, err := tool.Invoke(ctx, &CreateFileReq{Filename: filename, Content: "nope"})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error == "" {
		t.Fatalf("expected resp.Error to be set when approval is denied")
	}
	if _, statErr := os.Stat(filename); statErr == nil {
		t.Fatalf("expected file not to be created when approval is denied")
	}
}

func Test_CreateFileTool_Invoke_AllowsApproval_WritesFile(t *testing.T) {
	tmp := t.TempDir()
	ctx := types.WithWorkspacePwd(context.Background(), tmp)
	filename := filepath.Join(tmp, "ok.txt")

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true}}
	tool := CreateFileTool{approver: fa}

	resp, err := tool.Invoke(ctx, &CreateFileReq{Filename: filename, Content: "hello"})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("expected resp.Error empty; got %q", resp.Error)
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("unexpected file contents; want %q got %q", "hello", string(b))
	}
}

func Test_AppendFileTool_Invoke_AppendsToExistingFile(t *testing.T) {
	tmp := t.TempDir()
	ctx := types.WithWorkspacePwd(context.Background(), tmp)
	filename := filepath.Join(tmp, "append.txt")
	if err := os.WriteFile(filename, []byte("a"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true}}
	tool := AppendFileTool{approver: fa}

	resp, err := tool.Invoke(ctx, &AppendFileReq{Filename: filename, Content: "b"})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("expected resp.Error empty; got %q", resp.Error)
	}

	b, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != "ab" {
		t.Fatalf("unexpected file contents; want %q got %q", "ab", string(b))
	}
}

func Test_DeleteFileTool_Invoke_DeletesFile(t *testing.T) {
	tmp := t.TempDir()
	ctx := types.WithWorkspacePwd(context.Background(), tmp)
	filename := filepath.Join(tmp, "del.txt")
	if err := os.WriteFile(filename, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true}}
	tool := DeleteFileTool{approver: fa}

	resp, err := tool.Invoke(ctx, &DeleteFileReq{Filename: filename})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("expected resp.Error empty; got %q", resp.Error)
	}
	if _, statErr := os.Stat(filename); statErr == nil {
		t.Fatalf("expected file to be deleted")
	}
}

func Test_ReadFileTool_Invoke_ReadsContent_WithEOFError(t *testing.T) {
	tmp := t.TempDir()
	ctx := types.WithWorkspacePwd(context.Background(), tmp)
	filename := filepath.Join(tmp, "read.txt")
	if err := os.WriteFile(filename, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true}}
	tool := ReadFileTool{approver: fa}

	resp, err := tool.Invoke(ctx, &ReadFileReq{Filename: filename, StartOffset: 0, NumBytes: 100})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error == "" || !strings.Contains(strings.ToLower(resp.Error), "eof") {
		t.Fatalf("expected EOF in resp.Error due to short file; got %q", resp.Error)
	}
	if resp.Content.Content != "hello" {
		t.Fatalf("unexpected content; want %q got %q", "hello", resp.Content.Content)
	}
	if resp.Content.UntruncatedContentLen != len("hello") {
		t.Fatalf("unexpected UntruncatedContentLen; want %d got %d", len("hello"), resp.Content.UntruncatedContentLen)
	}
	if resp.Content.WasTruncated {
		t.Fatalf("did not expect truncation")
	}
}

func Test_ReadFileTool_Invoke_RefusesSymlinkEscape(t *testing.T) {
	tmp := t.TempDir()
	ctx := types.WithWorkspacePwd(context.Background(), tmp)

	// Create a file outside the workspace.
	outsideDir := t.TempDir()
	secretPath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(secret): %v", err)
	}

	// Create a symlink inside the workspace pointing to outsideDir.
	linkPath := filepath.Join(tmp, "leak")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	fa := &fakeApprover{decision: am.ApprovalDecision{Allowed: true}}
	tool := ReadFileTool{approver: fa}

	resp, err := tool.Invoke(ctx, &ReadFileReq{Filename: "leak/secret.txt", StartOffset: 0, NumBytes: 100})
	if err != nil {
		t.Fatalf("expected err=nil; got %v", err)
	}
	if resp.Error == "" {
		t.Fatalf("expected resp.Error to be set due to symlink escape")
	}
	if strings.Contains(resp.Content.Content, "secret") {
		t.Fatalf("expected secret not to be read via symlink")
	}
}
