/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package fsatomic

import (
	"context"
	"io/fs"
)

// Version is an opaque identifier for a file's contents/metadata at a point in time.
//
// Callers must treat Version as an opaque token and must not parse or
// interpret its contents.
//
// Implementations may derive Version from filesystem metadata, a content hash,
// an object-store ETag, or other backend-specific mechanisms.
type Version string

// File describes a persisted file as observed or written by an AtomicFS.
//
// Version is the value to use for optimistic concurrency via WriteFileCAS.
type File struct {
	Path    string
	Version Version
	Perm    fs.FileMode
}

// AtomicFS provides crash-safe, cross-process-safe, atomic file operations.
//
// Semantics (intended for local filesystems; backends may be best-effort):
//
//   - Atomicity: after WriteFile/WriteFileCAS returns nil, readers must observe
//     either the old complete contents or the new complete contents, never
//     partially written/truncated data.
//
//   - Durability: successful writes are intended to survive process crashes and
//     power failures. Local filesystem implementations should use a temp-file
//
//   - fsync + rename + fsync(parent-dir) discipline.
//
//   - Mutual exclusion: operations must be safe when multiple goroutines and
//     multiple gptcli processes on the same host concurrently access the same
//     paths. Locking is implicit and implementation-defined (e.g., advisory
//     file locks).
//
// Notes for non-local backends:
//
//   - Cross-host mutual exclusion is out of scope; AtomicFS is only required to
//     coordinate across processes on the same host.
//   - Some backends may not be able to provide the same durability guarantees
//     as a local filesystem.
//
// Callers that do read-modify-write should prefer the CAS API to avoid lost
// updates.
type AtomicFS interface {
	// ReadFile returns the contents of path and an opaque Version suitable for
	// optimistic concurrency control with WriteFileCAS.
	//
	// If the file does not exist, it should return (nil, File{Path: path}, ErrNotFound).
	ReadFile(ctx context.Context, path string) (data []byte, f File, err error)

	// WriteFile atomically replaces the contents of path.
	//
	// If the file does not exist, it is created.
	WriteFile(ctx context.Context, path string, data []byte, perm fs.FileMode) (f File, err error)

	// WriteFileCAS performs a compare-and-swap write.
	//
	// The write succeeds only if the current file Version matches expected.
	// If expected == "", the write succeeds only if the file does not exist.
	//
	// On a version mismatch, it returns ErrConflict.
	WriteFileCAS(ctx context.Context, path string, expected Version, data []byte, perm fs.FileMode) (f File, err error)
}
