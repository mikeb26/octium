/* Copyright © 2025 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package ui

// displayPos describes a cursor location in terms of display-wrapped rows.
//
// displayLineIdx is the 0-based index into the rendered (wrapped) rows.
// x is the 0-based column within that wrapped row (excluding the scrollbar
// column). startCol is the 0-based logical rune index in the underlying
// logical line where this wrapped row begins.
type displayPos struct {
	displayLineIdx int
	logicalLineIdx int
	startCol       int
	segLen         int
	x              int
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// contentTextWidth returns the number of columns available for text in the
// frame's content area, always reserving the last content column for the
// scrollbar.
func (f *Frame) contentTextWidth() int {
	_, _, _, contentW := f.contentBounds()
	textW := contentW - 1
	if textW < 1 {
		textW = 1
	}
	return textW
}

// totalDisplayLines returns the number of display rows produced by rendering
// the current logical buffer with soft wrapping.
func (f *Frame) totalDisplayLines(textWidth int) int {
	if len(f.lines) == 0 {
		return 0
	}
	if textWidth < 1 {
		textWidth = 1
	}

	display := f.buildDisplayLines(textWidth)
	return len(display)
}

// cursorDisplayPos computes where the current logical cursor would land in the
// rendered, wrapped view.
func (f *Frame) cursorDisplayPos(textWidth int) displayPos {
	pos := displayPos{displayLineIdx: 0, logicalLineIdx: 0, startCol: 0, segLen: 0, x: 0}
	if len(f.lines) == 0 {
		return pos
	}
	if textWidth < 1 {
		textWidth = 1
	}

	lineIdx := clampInt(f.cursorLine, 0, len(f.lines)-1)
	col := f.cursorCol
	if col < 0 {
		col = 0
	}
	lineRunes := f.lines[lineIdx].Runes
	if col > len(lineRunes) {
		col = len(lineRunes)
	}

	display := f.buildDisplayLines(textWidth)
	for di := 0; di < len(display); di++ {
		dl := display[di]
		if dl.logicalIdx != lineIdx {
			continue
		}
		if col < dl.startCol {
			continue
		}
		segEnd := dl.startCol + dl.segContentLen
		isLastSeg := !dl.continues
		if col < segEnd || (isLastSeg && col == segEnd) {
			pos.displayLineIdx = di
			pos.logicalLineIdx = dl.logicalIdx
			pos.startCol = dl.startCol
			pos.segLen = dl.segContentLen
			x := col - dl.startCol
			if x < 0 {
				x = 0
			}
			pos.x = dl.indentLen + x
			return pos
		}
	}

	// Fallback: end of buffer.
	if len(display) > 0 {
		last := display[len(display)-1]
		pos.displayLineIdx = len(display) - 1
		pos.logicalLineIdx = last.logicalIdx
		pos.startCol = last.startCol
		pos.segLen = last.segContentLen
		pos.x = last.indentLen + last.segContentLen
		return pos
	}
	return pos
}

// displayIndexToCursor maps a desired display row index and x position (column
// within that row) back to a logical cursor position.
func (f *Frame) displayIndexToCursor(textWidth, displayLineIdx, x int) (line, col int) {
	if len(f.lines) == 0 {
		return 0, 0
	}
	if textWidth < 1 {
		textWidth = 1
	}

	display := f.buildDisplayLines(textWidth)
	total := len(display)
	if total <= 0 {
		return 0, 0
	}
	displayLineIdx = clampInt(displayLineIdx, 0, total-1)
	if x < 0 {
		x = 0
	}

	dl := display[displayLineIdx]
	lineIdx := dl.logicalIdx
	colX := x - dl.indentLen
	if colX < 0 {
		colX = 0
	}
	if colX > dl.segContentLen {
		colX = dl.segContentLen
	}
	return lineIdx, dl.startCol + colX
}
