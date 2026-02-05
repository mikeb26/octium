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

func TestListRemoteRepos_ParsesGitRemoteV(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_REMOTE_V_OUT": "" +
			"origin\thttps://example.com/a/b.git (fetch)\n" +
			"origin\thttps://example.com/a/b.git (push)\n" +
			"upstream https://example.com/u/p.git (fetch)\n" +
			"upstream https://example.com/u/p.git (push)\n",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	got, err := c.ListRemoteRepos(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 remotes, got %d: %#v", len(got), got)
	}

	// Sorted by name.
	if got[0].Name != "origin" || got[1].Name != "upstream" {
		t.Fatalf("unexpected ordering: %#v", got)
	}

	expOrigin := scm.RemoteRepo{Name: "origin", FetchURL: "https://example.com/a/b.git", PushURL: "https://example.com/a/b.git"}
	if got[0] != expOrigin {
		t.Fatalf("origin mismatch: got %#v exp %#v", got[0], expOrigin)
	}
}

func TestFetchRemoteRepo_InvokesGitFetch(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{})
	t.Cleanup(cleanup)

	c := NewClient()
	if err := c.FetchRemoteRepo(context.Background(), t.TempDir(), "origin"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	logs := strings.Join(readMockGitLog(t, logPath), "\n")
	if !strings.Contains(logs, "fetch origin") {
		t.Fatalf("expected fetch origin in logs, got %#v", logs)
	}
}

func TestFetchRemoteRepo_ReturnsErrRemoteNameRequired(t *testing.T) {
	c := NewClient()
	err := c.FetchRemoteRepo(context.Background(), t.TempDir(), "")
	if !errors.Is(err, ErrRemoteNameRequired) {
		t.Fatalf("expected ErrRemoteNameRequired, got %v", err)
	}
}

func TestListBranches_Local(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_BRANCH_LIST_OUT": "" +
			"main\n" +
			"dev\n",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	branches, err := c.ListBranches(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Join(branches, ",") != "dev,main" {
		t.Fatalf("unexpected branches: %#v", branches)
	}
}

func TestListBranches_Remote(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_BRANCH_LIST_OUT": "" +
			"origin/main\n" +
			"origin/dev\n" +
			"other/ignored\n",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	branches, err := c.ListBranches(context.Background(), t.TempDir(), "origin")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Join(branches, ",") != "origin/dev,origin/main,other/ignored" {
		t.Fatalf("unexpected branches: %#v", branches)
	}

	logs := strings.Join(readMockGitLog(t, logPath), "\n")
	if !strings.Contains(logs, "branch --remotes --list origin/* --format=%(refname:short)") {
		t.Fatalf("expected branch list invocation in logs, got %#v", logs)
	}
}

func TestMerge_ReturnsErrorWhenLocalChangesPresent(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_STATUS_OUT": "" +
			"# branch.upstream origin/main\n" +
			"1 .M N... 100644 100644 100644 abcdef abcdef file.txt\n",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.Merge(context.Background(), t.TempDir(), "", "")
	if err == nil {
		t.Fatalf("expected error")
	}

	logs := strings.Join(readMockGitLog(t, logPath), "\n")
	if strings.Contains(logs, "merge --no-edit") {
		t.Fatalf("expected merge not to be invoked, logs: %#v", logs)
	}
}

func TestMerge_ReturnsErrorWhenUpstreamMissing(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_STATUS_OUT": "" +
			"# branch.oid head123\n" +
			"# branch.head main\n",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.Merge(context.Background(), t.TempDir(), "", "")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRepoSyncStatus_ParsesUpstreamAndDirtyAndOp(t *testing.T) {
	absGitDir := t.TempDir()
	logPath, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_STATUS_OUT": "" +
			"# branch.upstream origin/main\n" +
			"# branch.ab +2 -3\n" +
			"? untracked.txt\n",
		"MOCK_GIT_BRANCH":      "main",
		"MOCK_GIT_ABS_GIT_DIR": absGitDir,
	})
	t.Cleanup(cleanup)
	_ = logPath

	c := NewClient()
	st, err := c.RepoSyncStatus(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if st.UpstreamRemote != "origin" || st.UpstreamBranch != "main" {
		t.Fatalf("unexpected upstream: %#v", st)
	}
	if st.LocalBranch != "main" {
		t.Fatalf("unexpected local branch: %#v", st)
	}
	if st.Ahead != 2 || st.Behind != 3 {
		t.Fatalf("unexpected ahead/behind: %#v", st)
	}
	if !st.HasUncommittedChanges {
		t.Fatalf("expected uncommitted changes")
	}
	if st.Operation != scm.RepoOperationNormal {
		t.Fatalf("expected normal operation")
	}
}

func TestMerge_ReturnsMergeConflictsOnExit1AndLeavesState(t *testing.T) {
	absGitDir := t.TempDir()
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_STATUS_OUT": "" +
			"# branch.upstream origin/main\n" +
			"# branch.ab +0 -1\n",
		"MOCK_GIT_ABS_GIT_DIR": absGitDir,
		"MOCK_GIT_MERGE_EXIT":  "1",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.Merge(context.Background(), t.TempDir(), "", "")
	if !errors.Is(err, scm.ErrMergeConflicts) {
		t.Fatalf("expected ErrMergeConflicts, got %v", err)
	}
}
