/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindNearestGitRepoRoot_FindsNearestParentWithDotGitDir(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "somerepo")
	sub := filepath.Join(repo, "a", "b")
	assert.NoError(t, os.MkdirAll(sub, 0o755))
	assert.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))

	got, ok, err := findNearestGitRepoRoot(sub)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, repo, got)
}

func TestFindNearestGitRepoRoot_FindsNearestParentWithDotGitFile(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "somerepo")
	sub := filepath.Join(repo, "a", "b")
	assert.NoError(t, os.MkdirAll(sub, 0o755))
	assert.NoError(t, os.MkdirAll(repo, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: /tmp/elsewhere"), 0o644))

	got, ok, err := findNearestGitRepoRoot(sub)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, repo, got)
}

func TestFindNearestGitRepoRoot_NotFound(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	assert.NoError(t, os.MkdirAll(sub, 0o755))

	got, ok, err := findNearestGitRepoRoot(sub)
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, filepath.Clean(sub), got)
}
