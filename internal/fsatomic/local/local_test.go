/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mikeb26/octium/internal/fsatomic"
)

func TestFS_ReadFile_NotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.txt")

	lfs := New()
	b, f, err := lfs.ReadFile(ctx, path)
	if !errors.Is(err, fsatomic.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if b != nil {
		t.Fatalf("expected nil bytes, got %d", len(b))
	}
	if f.Path != path {
		t.Fatalf("expected File.Path %q, got %q", path, f.Path)
	}
}

func TestFS_WriteFile_ReadFile_RoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "file.txt")

	lfs := New()
	f1, err := lfs.WriteFile(ctx, path, []byte("hello"), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if f1.Path != path {
		t.Fatalf("expected Path %q, got %q", path, f1.Path)
	}
	if f1.Version == "" {
		t.Fatalf("expected non-empty Version")
	}

	b, f2, err := lfs.ReadFile(ctx, path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(b))
	}
	if f2.Version != f1.Version {
		t.Fatalf("expected version %q, got %q", f1.Version, f2.Version)
	}
}

func TestFS_WriteFile_PreservesPerms(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	lfs := New()
	// When overwriting, preserve the existing file's permissions.
	perm, err := existingPerm(path)
	if err != nil {
		t.Fatalf("existingPerm: %v", err)
	}
	if _, err := lfs.WriteFile(ctx, path, []byte("new"), perm); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := st.Mode().Perm(); got != 0o640 {
		t.Fatalf("expected perms %o, got %o", 0o640, got)
	}
}

func TestFS_WriteFileCAS_CreateOnlyAndUpdate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	lfs := New()

	// Create-only: expected=="" should create when absent.
	f1, err := lfs.WriteFileCAS(ctx, path, "", []byte("v1"), 0o600)
	if err != nil {
		t.Fatalf("WriteFileCAS(create): %v", err)
	}
	if f1.Version == "" {
		t.Fatalf("expected non-empty Version")
	}

	// Create-only should conflict when present.
	if _, err := lfs.WriteFileCAS(ctx, path, "", []byte("should-fail"), 0o600); !errors.Is(err, fsatomic.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Update with wrong expected should conflict.
	if _, err := lfs.WriteFileCAS(ctx, path, fsatomic.Version("bogus"), []byte("should-fail"), 0o600); !errors.Is(err, fsatomic.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Update with correct expected should succeed.
	f2, err := lfs.WriteFileCAS(ctx, path, f1.Version, []byte("v2"), 0o600)
	if err != nil {
		t.Fatalf("WriteFileCAS(update): %v", err)
	}
	if f2.Version == f1.Version {
		t.Fatalf("expected version to change")
	}

	b, _, err := lfs.ReadFile(ctx, path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != "v2" {
		t.Fatalf("expected %q, got %q", "v2", string(b))
	}
}

func TestLockFile_BlocksUntilUnlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("lock file is best-effort/no-op on windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")

	lf1, err := newLockFile(target)
	if err != nil {
		t.Fatalf("newLockFile 1: %v", err)
	}
	defer lf1.close()

	lf2, err := newLockFile(target)
	if err != nil {
		t.Fatalf("newLockFile 2: %v", err)
	}
	defer lf2.close()

	if err := lf1.lock(context.Background()); err != nil {
		t.Fatalf("lf1.lock: %v", err)
	}

	// Attempt to lock with a short deadline; it should time out while lf1 holds the lock.
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = lf2.lock(ctxTimeout)
	if err == nil {
		_ = lf2.unlock()
		t.Fatalf("expected lock attempt to fail due to context timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatalf("expected lock attempt to block for some time")
	}

	if err := lf1.unlock(); err != nil {
		t.Fatalf("lf1.unlock: %v", err)
	}

	// Now lock should succeed.
	ctxOK, cancelOK := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelOK()
	if err := lf2.lock(ctxOK); err != nil {
		t.Fatalf("lf2.lock after unlock: %v", err)
	}
	if err := lf2.unlock(); err != nil {
		t.Fatalf("lf2.unlock: %v", err)
	}
}
