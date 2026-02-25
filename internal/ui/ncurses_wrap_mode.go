/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package ui

import "strings"

// WrapMode controls how text is displayed when it exceeds available width.
type WrapMode int

const (
	// WrapModeInherit means "use the frame-level wrap mode".
	WrapModeInherit WrapMode = iota
	// WrapModeOff truncates content to the available width with no wrapping.
	WrapModeOff
	// WrapModeHard wraps strictly at rune boundaries. Continuation segments are
	// rendered with a trailing '\\' marker.
	WrapModeHard
	// WrapModeWord wraps at whitespace boundaries when possible.
	//
	// Normal word-wrap breaks do not include a continuation marker. If a single
	// token must be hard-wrapped (e.g. a long URL), those forced breaks *do*
	// include a trailing '\\' marker.
	WrapModeWord
)

func (m WrapMode) String() string {
	switch m {
	case WrapModeInherit:
		return "inherit"
	case WrapModeOff:
		return "off"
	case WrapModeWord:
		return "word"
	case WrapModeHard:
		return "hard"
	default:
		return "unknown"
	}
}

func (m WrapMode) FromString(s string) WrapMode {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "inherit":
		return WrapModeInherit
	case "off", "none", "truncate":
		return WrapModeOff
	case "word", "words":
		return WrapModeWord
	case "hard", "wrap":
		return WrapModeHard
	default:
		return WrapModeHard
	}
}
