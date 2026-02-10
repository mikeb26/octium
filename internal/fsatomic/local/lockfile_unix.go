//go:build !windows

/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
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
	// Block waiting for an advisory lock, but honor ctx cancellation.
	// We use a simple retry loop with backoff because unix.Flock is not context-aware.
	backoff := 10 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := unix.Flock(int(l.f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			time.Sleep(backoff)
			if backoff < 250*time.Millisecond {
				backoff *= 2
			}
			continue
		}
		return fmt.Errorf("lock %s: %w", l.path, err)
	}
}

func (l *lockFile) unlock() error {
	return unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
}

func (l *lockFile) close() error {
	return l.f.Close()
}
