/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

//go:build cgo

package ui

/*
#include <curses.h>

// Some curses implementations define A_ITALIC; others don't.
// If it's missing, we treat italics as unsupported (0).
static unsigned int octium_attr_italic() {
#ifdef A_ITALIC
    return (unsigned int)A_ITALIC;
#else
    return 0;
#endif
}
*/
import "C"

import gc "github.com/rthornton128/goncurses"

// AttrItalic returns the curses attribute bitmask for italic text.
//
// If italics are not supported by the underlying curses implementation,
// this returns 0 (no-op).
func AttrItalic() gc.Char {
	return gc.Char(C.octium_attr_italic())
}
