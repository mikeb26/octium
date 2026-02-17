/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

//go:build !cgo

package ui

import gc "github.com/rthornton128/goncurses"

// AttrItalic returns the curses attribute bitmask for italic text.
//
// In non-cgo builds, we cannot access platform-specific curses constants,
// so italics are treated as unsupported.
func AttrItalic() gc.Char {
	return 0
}
