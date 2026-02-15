//go:build !linux

/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package fsparanoid

import (
	"os"
)

func Open(rootAbs string, relPath string, how OpenHow) (*os.File, error) {
	return nil, ErrNotSupported
}

func Mkdir(rootAbs string, relDir string, perm os.FileMode) error {
	return ErrNotSupported
}

func Remove(rootAbs string, relPath string) error {
	return ErrNotSupported
}

func ReadFile(rootAbs string, relPath string) ([]byte, error) {
	return nil, ErrNotSupported
}

func WriteFile(rootAbs string, relPath string, content []byte, perm os.FileMode) error {
	return ErrNotSupported
}
