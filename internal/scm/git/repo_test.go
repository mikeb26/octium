/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikeb26/octium/internal/scm"
)

func TestInitRepo_CreatesDirAndRunsGitInit(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{})
	t.Cleanup(cleanup)

	dir := filepath.Join(t.TempDir(), "newrepo")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir not to exist")
	}

	c := NewClient()
	if err := c.InitRepo(context.Background(), dir); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("expected dir to exist after init")
	}

	logs := readMockGitLog(t, logPath)
	joined := strings.Join(logs, "\n")
	// buildGitArgs uses -C, so we should see init invoked (with no args).
	if !strings.Contains(joined, "init") {
		t.Fatalf("expected init in logs, got %#v", logs)
	}
}

func TestInitRepo_WrapsGitError(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_INIT_EXIT": "1",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.InitRepo(context.Background(), t.TempDir())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrFailedToExecuteGit) {
		t.Fatalf("expected ErrFailedToExecuteGit wrapper, got %v", err)
	}
}

func TestInitRepo_UsesTimeoutWhenNoDeadline(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_SLEEP_SECS": "0.25",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	c.Timeout = 20 * time.Millisecond

	err := c.InitRepo(context.Background(), t.TempDir())
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, ErrFailedToExecuteGit) {
		t.Fatalf("expected ErrFailedToExecuteGit wrapper, got %v", err)
	}
}

func TestCloneRepo_CreatesDirAndRunsGitClone(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{})
	t.Cleanup(cleanup)

	dir := filepath.Join(t.TempDir(), "cloned")

	c := NewClient()
	if err := c.CloneRepo(context.Background(), "https://example.com/org/repo.git", dir); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("expected dir to exist after clone")
	}

	logs := readMockGitLog(t, logPath)
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "clone https://example.com/org/repo.git ") {
		t.Fatalf("expected clone invocation in logs, got %#v", logs)
	}
}

func TestCloneRepo_WrapsGitError(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_CLONE_EXIT": "1",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.CloneRepo(context.Background(), "https://example.com/org/repo.git", t.TempDir())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrFailedToExecuteGit) {
		t.Fatalf("expected ErrFailedToExecuteGit wrapper, got %v", err)
	}
}

var _ scm.Client = (*Client)(nil)
