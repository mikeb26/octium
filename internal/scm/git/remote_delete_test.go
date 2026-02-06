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

func TestDeleteRemoteRepo_InvokesGitRemoteRemove(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.DeleteRemoteRepo(context.Background(), t.TempDir(), "origin")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	logs := strings.Join(readMockGitLog(t, logPath), "\n")
	if !strings.Contains(logs, "remote remove origin") {
		t.Fatalf("expected remote remove invocation in logs, got %#v", logs)
	}
}

func TestDeleteRemoteRepo_WrapsGitError(t *testing.T) {
	_, cleanup := setupMockGit(t, map[string]string{
		"MOCK_GIT_REMOTE_REMOVE_EXIT": "1",
	})
	t.Cleanup(cleanup)

	c := NewClient()
	err := c.DeleteRemoteRepo(context.Background(), t.TempDir(), "origin")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrFailedToExecuteGit) {
		t.Fatalf("expected ErrFailedToExecuteGit wrapper, got %v", err)
	}
}

func TestDeleteRemoteRepo_ReturnsErrRemoteNameRequired(t *testing.T) {
	c := NewClient()
	err := c.DeleteRemoteRepo(context.Background(), t.TempDir(), "")
	if !errors.Is(err, ErrRemoteNameRequired) {
		t.Fatalf("expected ErrRemoteNameRequired, got %v", err)
	}
}
