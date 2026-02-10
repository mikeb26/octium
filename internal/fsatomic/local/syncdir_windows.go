//go:build windows

/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package local

func syncDir(dir string) error {
	_ = dir
	return nil
}
