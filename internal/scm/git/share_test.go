/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"context"
	"strings"
	"testing"

	"github.com/mikeb26/octium/internal"
)

func TestShareRepo_SetsCoreSharedRepository(t *testing.T) {
	logPath, cleanup := setupMockGit(t, map[string]string{})
	t.Cleanup(cleanup)

	c := NewClient()
	if err := c.ShareRepo(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	logs := readMockGitLog(t, logPath)
	joined := strings.Join(logs, "\n")
	want := "config core.sharedRepository " + internal.CliSandboxGroupname
	if !strings.Contains(joined, want) {
		t.Fatalf("expected %q in logs, got %#v", want, logs)
	}
}
