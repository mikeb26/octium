//go:build windows

/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package local

import (
	"context"
	"os"
	"path/filepath"
)

type lockFile struct {
	path string
	f    *os.File
}

func newLockFile(targetPath string) (*lockFile, error) {
	lockPath := targetPath + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return &lockFile{path: lockPath, f: f}, nil
}

func (l *lockFile) lock(ctx context.Context) error {
	// Best-effort: Windows file locking semantics vary, and octium is primarily
	// targeting Unix-like systems. We rely on in-process locking here.
	_ = ctx
	return nil
}

func (l *lockFile) unlock() error { return nil }

func (l *lockFile) close() error { return l.f.Close() }
