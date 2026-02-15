/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package fsparanoid

import "os"

// OpenFlags represents high-level, cross-platform open intent.
//
// The Linux implementation translates these flags into openat2(2) OpenHow
// values and enforces confinement beneath a caller-provided root directory.
//
// On non-Linux platforms, Open currently fails closed.
// If/when macOS/Windows support is added, these flags should map to the
// strongest available platform primitives.
type OpenFlags uint32

const (
	OpenRead OpenFlags = 1 << iota
	OpenWrite
	OpenAppend
	OpenCreate
	OpenTrunc
	// OpenPath indicates the caller wants an O_PATH-style handle suitable for
	// unlink-at-empty-path and metadata checks.
	//
	// This is Linux-specific today.
	OpenPath
	// OpenDirectory requires the opened target to be a directory.
	OpenDirectory
)

type OpenHow struct {
	Flags OpenFlags
	Perm  os.FileMode
}
