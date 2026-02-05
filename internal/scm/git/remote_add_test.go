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
)

func TestAddRemoteRepo_InvokesGitRemoteAdd(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.AddRemoteRepo(context.Background(), t.TempDir(), "origin", "https://example.com/a/b.git")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	logs := strings.Join(readMockGitLog(t, logPath), "\n")
	if !strings.Contains(logs, "remote add origin https://example.com/a/b.git") {
		t.Fatalf("expected remote add invocation in logs, got %#v", logs)
	}
}

func TestAddRemoteRepo_WrapsGitError(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_REMOTE_ADD_EXIT": "1",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.AddRemoteRepo(context.Background(), t.TempDir(), "origin", "https://example.com/a/b.git")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrFailedToExecuteGit) {
		t.Fatalf("expected ErrFailedToExecuteGit wrapper, got %v", err)
	}
}

func TestAddRemoteRepo_ReturnsErrRemoteNameRequired(t *testing.T) {
	c := NewClient()
	err := c.AddRemoteRepo(context.Background(), t.TempDir(), "", "https://example.com/a/b.git")
	if !errors.Is(err, ErrRemoteNameRequired) {
		t.Fatalf("expected ErrRemoteNameRequired, got %v", err)
	}
}

func TestAddRemoteRepo_ReturnsErrRemoteURLRequired(t *testing.T) {
	c := NewClient()
	err := c.AddRemoteRepo(context.Background(), t.TempDir(), "origin", "")
	if !errors.Is(err, ErrRemoteURLRequired) {
		t.Fatalf("expected ErrRemoteURLRequired, got %v", err)
	}
}
