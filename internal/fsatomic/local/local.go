/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package local

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/mikeb26/octium/internal/fsatomic"
	"github.com/negrel/assert"
)

// FS implements fsatomic.AtomicFS for local filesystems.
//
// It provides:
//   - in-process mutual exclusion (per-path mutex)
//   - cross-process mutual exclusion on the same host (advisory lock file)
//   - crash-safe writes via write-temp + fsync + rename + fsync(parent-dir)
//
// Note: this implementation assumes the target path and its lock file live on
// the same filesystem. Atomic rename is only guaranteed within a single
// filesystem.
//
// Locking is per-path. If you need to update multiple files atomically as a
// single transaction, that is not supported by fsatomic.AtomicFS.
type FS struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func New() *FS {
	return &FS{locks: make(map[string]*sync.Mutex)}
}

func (l *FS) ReadFile(ctx context.Context, path string) ([]byte, fsatomic.File, error) {
	unlock, err := l.lockPath(ctx, path)
	if err != nil {
		return nil, fsatomic.File{Path: path}, err
	}
	defer unlock()

	b, perm, mtime, err := readFileBytesPermMtime(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fsatomic.File{Path: path}, fsatomic.ErrNotFound
		}
		return nil, fsatomic.File{Path: path}, err
	}
	v := versionForBytesMtime(b, mtime)
	return b, fsatomic.File{Path: path, Version: v, Perm: perm}, nil
}

func (l *FS) WriteFile(ctx context.Context, path string, data []byte, perm fs.FileMode) (fsatomic.File, error) {
	unlock, err := l.lockPath(ctx, path)
	if err != nil {
		return fsatomic.File{Path: path}, err
	}
	defer unlock()

	if err := writeFileAtomic(path, data, perm); err != nil {
		return fsatomic.File{Path: path}, err
	}

	mtime, err := statMtime(path)
	if err != nil {
		return fsatomic.File{Path: path}, err
	}

	return fsatomic.File{Path: path, Version: versionForBytesMtime(data, mtime), Perm: perm}, nil
}

func (l *FS) WriteFileCAS(ctx context.Context, path string, expected fsatomic.Version, data []byte, perm fs.FileMode) (fsatomic.File, error) {
	unlock, err := l.lockPath(ctx, path)
	if err != nil {
		return fsatomic.File{Path: path}, err
	}
	defer unlock()

	if expected == "" {
		if _, err := os.Stat(path); err == nil {
			return fsatomic.File{Path: path}, fsatomic.ErrConflict
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fsatomic.File{Path: path}, err
		}

		if err := writeFileAtomic(path, data, perm); err != nil {
			return fsatomic.File{Path: path}, err
		}

		mtime, err := statMtime(path)
		if err != nil {
			return fsatomic.File{Path: path}, err
		}

		return fsatomic.File{Path: path, Version: versionForBytesMtime(data, mtime), Perm: perm}, nil
	}

	cur, _, mtime, err := readFileBytesPermMtime(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fsatomic.File{Path: path}, fsatomic.ErrConflict
		}
		return fsatomic.File{Path: path}, err
	}

	if versionForBytesMtime(cur, mtime) != expected {
		return fsatomic.File{Path: path}, fsatomic.ErrConflict
	}

	if err := writeFileAtomic(path, data, perm); err != nil {
		return fsatomic.File{Path: path}, err
	}

	newMtime, err := statMtime(path)
	if err != nil {
		return fsatomic.File{Path: path}, err
	}

	return fsatomic.File{Path: path, Version: versionForBytesMtime(data, newMtime), Perm: perm}, nil
}

// lockPath acquires mutual exclusion for operations on path.
//
// The lock is held for the duration of the returned unlock function and is:
//   - process-local: a per-path *sync.Mutex prevents concurrent goroutines from
//     interleaving read/modify/write sequences.
//   - host-local: an advisory lock file (path + ".lock") is used to coordinate
//     with other octium processes operating on the same threads directory.
//
// The lock acquisition order is always (1) in-process mutex then (2) lock file.
// Keeping a single ordering avoids deadlocks if future code ever composes
// operations.
//
// The returned func is the corresponding unlocker; callers should invoke it
// exactly once (typically via defer) to release both the advisory lock file and
// the in-process mutex.
func (l *FS) lockPath(ctx context.Context, path string) (func(), error) {
	m := l.getMutex(path)
	m.Lock()

	lf, err := newLockFile(path)
	if err != nil {
		m.Unlock()
		return nil, err
	}
	if err := lf.lock(ctx); err != nil {
		_ = lf.close()
		m.Unlock()
		return nil, err
	}

	return func() {
		_ = lf.unlock()
		_ = lf.close()
		m.Unlock()
	}, nil
}

func (l *FS) getMutex(path string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()

	assert.NotNil(l.locks)

	m, ok := l.locks[path]
	if !ok {
		m = &sync.Mutex{}
		l.locks[path] = m
	}
	return m
}

func versionForBytesMtime(b []byte, mtime time.Time) fsatomic.Version {
	h := sha256.New()
	_, _ = h.Write(b)

	// Include the file's mtime in the digest so that metadata-only changes
	// (e.g. tools touching files) are reflected in the Version token.
	var mt [8]byte
	binary.BigEndian.PutUint64(mt[:], uint64(mtime.UTC().UnixNano()))
	_, _ = h.Write(mt[:])

	return fsatomic.Version(hex.EncodeToString(h.Sum(nil)))
}

func readFileBytesPermMtime(path string) ([]byte, fs.FileMode, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, 0, time.Time{}, err
	}

	st, err := f.Stat()
	if err != nil {
		return nil, 0, time.Time{}, err
	}

	return b, st.Mode().Perm(), st.ModTime(), nil
}

func statMtime(path string) (time.Time, error) {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return st.ModTime(), nil
}

func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmpPath := tmpName(path)
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}

	success := false
	defer func() {
		if !success {
			_ = f.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	if err := syncDir(dir); err != nil {
		// Some platforms/filesystems may not support directory fsync. Allow best-effort
		// on Windows.
		if runtime.GOOS != "windows" {
			return err
		}
	}

	success = true
	return nil
}

func existingPerm(path string) (fs.FileMode, error) {
	st, err := os.Stat(path)
	if err == nil {
		return st.Mode().Perm(), nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return 0o600, nil
	}
	return 0, err
}

func tmpName(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	return filepath.Join(dir, fmt.Sprintf(".%s.tmp.%d.%d", base, os.Getpid(), rand.Uint64()))
}
