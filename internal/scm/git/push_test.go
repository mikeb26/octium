/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mikeb26/gptcli/internal/scm"
)

func TestPush_UsesUpstreamRemoteAndBranchWhenUnspecified(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_STATUS_OUT": "" +
			"# branch.upstream origin/main\n" +
			"# branch.ab +0 -0\n",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	if err := c.Push(context.Background(), t.TempDir(), "", ""); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	logs := strings.Join(readMockGitLog(t, logPath), "\n")
	if !strings.Contains(logs, "push origin HEAD:refs/heads/main") {
		t.Fatalf("expected push origin HEAD:refs/heads/main in logs, got %#v", logs)
	}
}

func TestPush_ReturnsErrorWhenUpstreamMissing(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_STATUS_OUT": "" +
			"# branch.oid head123\n" +
			"# branch.head main\n",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.Push(context.Background(), t.TempDir(), "", "")
	if err == nil || !errors.Is(err, ErrNoUpstreamConfigured) {
		t.Fatalf("expected ErrNoUpstreamConfigured, got %v", err)
	}
}

func TestPush_ReturnsErrorWhenLocalChangesPresent(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_STATUS_OUT": "" +
			"# branch.upstream origin/main\n" +
			"1 .M N... 100644 100644 100644 abcdef abcdef file.txt\n",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.Push(context.Background(), t.TempDir(), "", "")
	if err == nil {
		t.Fatalf("expected error")
	}

	logs := strings.Join(readMockGitLog(t, logPath), "\n")
	if strings.Contains(logs, "push ") {
		t.Fatalf("expected push not to be invoked, logs: %#v", logs)
	}
}

func TestPush_ReturnsErrPushRejectedOnExit1(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_STATUS_OUT": "" +
			"# branch.upstream origin/main\n" +
			"# branch.ab +0 -0\n",
		"MOCK_GIT_PUSH_EXIT": "1",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.Push(context.Background(), t.TempDir(), "", "")
	if err == nil || !errors.Is(err, scm.ErrPushRejected) {
		t.Fatalf("expected scm.ErrPushRejected, got %v", err)
	}
}

func TestPush_RequiresBothRemoteAndBranchWhenSpecified(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_STATUS_OUT": "" +
			"# branch.upstream origin/main\n" +
			"# branch.ab +0 -0\n",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.Push(context.Background(), t.TempDir(), "origin", "")
	if err == nil || !errors.Is(err, ErrRemoteAndBranchRequired) {
		t.Fatalf("expected ErrRemoteAndBranchRequired, got %v", err)
	}
}
