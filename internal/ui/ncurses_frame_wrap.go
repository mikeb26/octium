/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package ui

import (
	"unicode"

	gc "github.com/rthornton128/goncurses"
)

// frameDisplayLine is a render-time representation of a line segment.
//
// It is designed to:
//   - support multiple wrapping modes (hard/word/off)
//   - preserve cursor mapping (logical col -> display col) even when we add
//     indentation for hanging list items.
type frameDisplayLine struct {
	runes []rune
	attr  gc.Char

	logicalIdx int
	startCol   int

	// indentLen is the number of indentation runes inserted at the start of
	// runes. These are not part of the underlying logical line.
	indentLen int

	// segContentLen is the number of *content* runes in this display line (i.e.
	// excluding the indent).
	segContentLen int

	// continues indicates the underlying logical line continues onto another
	// display row.
	continues bool

	// showMarker indicates whether this display row should render the trailing
	// '\\' continuation marker.
	showMarker bool
}

func (f *Frame) buildDisplayLines(textWidth int) []frameDisplayLine {
	if textWidth < 1 {
		textWidth = 1
	}
	if len(f.lines) == 0 {
		return nil
	}

	out := make([]frameDisplayLine, 0, len(f.lines))
	for li, line := range f.lines {
		attr := line.Attr
		if attr == 0 {
			attr = gc.A_NORMAL
		}

		mode := f.WrapMode
		if line.WrapMode != WrapModeInherit {
			mode = line.WrapMode
		}

		switch mode {
		case WrapModeOff:
			out = append(out, buildOffDisplayLines(li, line.Runes, textWidth, attr)...)
		case WrapModeWord:
			out = append(out, buildWordDisplayLines(li, line.Runes, textWidth, attr)...)
		case WrapModeHard:
			fallthrough
		default:
			out = append(out, buildHardDisplayLines(li, line.Runes, textWidth, attr)...)
		}
	}

	return out
}

func buildOffDisplayLines(logicalIdx int, content []rune, textWidth int, attr gc.Char) []frameDisplayLine {
	content = ExpandTabsRunes(content, DefaultTabWidth)
	// WrapModeOff is rendered as a single display row with truncation at render
	// time. We intentionally keep segContentLen equal to the *full* logical line
	// length so cursor mapping remains correct even when the visible row is
	// truncated.
	return []frameDisplayLine{{
		runes:         content,
		attr:          attr,
		logicalIdx:    logicalIdx,
		startCol:      0,
		indentLen:     0,
		segContentLen: len(content),
		continues:     false,
		showMarker:    false,
	}}
}

func buildHardDisplayLines(logicalIdx int, content []rune, textWidth int, attr gc.Char) []frameDisplayLine {
	segments, wrappedFlags := WrapRunesWithContinuation(content, textWidth)
	out := make([]frameDisplayLine, 0, len(segments))
	col := 0
	for si, seg := range segments {
		dl := frameDisplayLine{
			runes:         seg,
			attr:          attr,
			logicalIdx:    logicalIdx,
			startCol:      col,
			indentLen:     0,
			segContentLen: len(seg),
			continues:     wrappedFlags[si],
			showMarker:    wrappedFlags[si],
		}
		out = append(out, dl)
		col += len(seg)
	}
	return out
}

func buildWordDisplayLines(logicalIdx int, content []rune, textWidth int, attr gc.Char) []frameDisplayLine {
	content = ExpandTabsRunes(content, DefaultTabWidth)

	// Always return at least one line so empty content occupies a row.
	if len(content) == 0 {
		return []frameDisplayLine{{
			runes:         []rune{},
			attr:          attr,
			logicalIdx:    logicalIdx,
			startCol:      0,
			indentLen:     0,
			segContentLen: 0,
			continues:     false,
			showMarker:    false,
		}}
	}

	indentLen := computeHangingIndent(content, textWidth)
	indentRunes := make([]rune, indentLen)
	for i := range indentRunes {
		indentRunes[i] = ' '
	}

	out := make([]frameDisplayLine, 0, 8)
	start := 0
	lineIdx := 0
	for start < len(content) {
		avail := textWidth
		useIndent := lineIdx > 0 && indentLen > 0
		if useIndent {
			avail = textWidth - indentLen
			if avail < 1 {
				avail = 1
			}
		}

		seg, next, forcedBreak := nextWordWrapSegment(content, start, avail)
		continues := next < len(content)
		showMarker := forcedBreak && continues

		var runes []rune
		if useIndent {
			runes = append(append([]rune{}, indentRunes...), seg...)
		} else {
			runes = seg
		}

		out = append(out, frameDisplayLine{
			runes:         runes,
			attr:          attr,
			logicalIdx:    logicalIdx,
			startCol:      start,
			indentLen:     0,
			segContentLen: len(seg),
			continues:     continues,
			showMarker:    showMarker,
		})

		// Only treat indentation as "inserted" for cursor mapping on subsequent
		// wrapped lines.
		if useIndent {
			out[len(out)-1].indentLen = indentLen
		}

		start = next
		lineIdx++
	}

	return out
}

func computeHangingIndent(content []rune, width int) int {
	_ = width

	// We only apply hanging indent for common list prefixes and only when the
	// prefix is on the first line.
	//
	// Examples:
	//   - "- foo" => indent 2
	//   - "* foo" => indent 2
	//   - "1. foo" => indent len("1. ")
	//
	// If we can't recognize a safe prefix, return 0.
	i := 0
	for i < len(content) && (content[i] == ' ' || content[i] == '\t') {
		i++
	}
	if i >= len(content) {
		return 0
	}

	// Bullet list.
	if (content[i] == '-' || content[i] == '*' || content[i] == '•') && i+1 < len(content) && content[i+1] == ' ' {
		return i + 2
	}

	// Numbered list: digits+"."+space
	j := i
	for j < len(content) && unicode.IsDigit(content[j]) {
		j++
	}
	if j > i && j+1 < len(content) && content[j] == '.' && content[j+1] == ' ' {
		return j + 2
	}

	return 0
}

// nextWordWrapSegment returns a segment that fits within avail columns starting
// at start. It prefers breaking on whitespace.
//
// It also consumes leading whitespace at start (so wrapped lines don't begin
// with spaces) but still advances the logical start index accordingly.
func nextWordWrapSegment(content []rune, start, avail int) (seg []rune, next int, forcedBreak bool) {
	if avail < 1 {
		avail = 1
	}
	if start >= len(content) {
		return []rune{}, start, false
	}

	// Consume leading whitespace.
	i := start
	for i < len(content) {
		r := content[i]
		if r != ' ' && r != '\t' {
			break
		}
		i++
	}
	start = i
	if start >= len(content) {
		return []rune{}, len(content), false
	}

	// If the remaining content fits, take it all.
	remaining := len(content) - start
	if remaining <= avail {
		return content[start:], len(content), false
	}

	limit := start + avail
	if limit > len(content) {
		limit = len(content)
	}

	// Find last whitespace within [start, limit).
	breakAt := -1
	for k := start; k < limit; k++ {
		r := content[k]
		if r == ' ' || r == '\t' {
			breakAt = k
		}
	}

	if breakAt != -1 {
		// Break at last whitespace we found, trimming trailing whitespace.
		end := breakAt
		for end > start {
			r := content[end-1]
			if r != ' ' && r != '\t' {
				break
			}
			end--
		}
		if end <= start {
			// All whitespace; fallback to hard split.
			return content[start:limit], limit, true
		}
		return content[start:end], breakAt + 1, false
	}

	// No whitespace in the available range: hard split (forced).
	return content[start:limit], limit, true
}
