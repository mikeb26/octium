/* Copyright © 2025 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package ui

import (
	"fmt"

	gc "github.com/rthornton128/goncurses"
)

// FrameLine represents a single logical line of text within a Frame's
// content area. Runes holds the textual content, while Attr carries any
// per-line ncurses attributes (color pair, bold, etc.). Callers that do
// not need styling can leave Attr as gc.A_NORMAL.
type FrameLine struct {
	Runes []rune
	Attr  gc.Char
	// WrapMode optionally overrides the frame-level wrap mode for this
	// particular logical line.
	//
	// When WrapMode is WrapModeInherit (default), the Frame's WrapMode is
	// used.
	WrapMode WrapMode
}

// Frame represents a logical X-by-Y window region backed by its own
// ncurses subwindow. It provides optional border, cursor, input buffer,
// and vertical scrollbar support so that callers can render scrollable
// text/input areas without reimplementing these behaviors.
//
// A Frame always reserves its last column for a potential vertical
// scrollbar, even when the content currently fits and no scrollbar is
// drawn. This keeps the layout stable as content grows.
//
// The public API is intentionally small and opinionated: callers create
// a Frame with the desired size and behavior flags, then:
//   - call Render with optional external content (e.g. history), and/or
//   - use the input methods (InsertRune, Backspace, etc.) if HasInput is
//     true.
//
// Cursor visibility is left to higher-level code; callers can choose
// whether to place the terminal cursor for a given Render call via the
// showCursor parameter.
type Frame struct {
	Win *gc.Window
	// pan is an optional panel that tracks this frame's window in the
	// global panel stack. Using panels allows higher-level code (such as
	// Modal) to hide/delete the top-most window and then call
	// goncurses.UpdatePanels() to restore whatever windows were
	// previously underneath without needing to know about them
	// explicitly.
	pan *gc.Panel

	// Outer dimensions, including any border.
	Height int
	Width  int

	// Behavior flags.
	HasBorder bool
	HasCursor bool
	HasInput  bool

	// WrapMode controls how logical lines are displayed when they exceed
	// the frame's available text width.
	WrapMode WrapMode

	// Content buffer for the frame. Editable frames mutate the Runes
	// fields in-place; read-only frames typically replace the entire
	// slice via SetLines.
	lines      []FrameLine
	cursorLine int
	cursorCol  int
	// scroll is the first visible *display* row index within the content area.
	//
	// Since frames render-time wrap logical lines into potentially multiple
	// display rows, scroll is tracked in display coordinates.
	scroll int
}

// SetLines replaces the frame's backing content with the provided
// slice. It is primarily intended for read-only frames such as history
// panes, but editable frames may also use it to seed initial content.
//
// The cursor and scroll positions are clamped to remain valid for the
// new content. Callers that want a specific starting position (e.g. at
// the end of the buffer) can invoke MoveEnd or MoveHome after
// SetLines.
func (f *Frame) SetLines(lines []FrameLine) {
	if f == nil {
		return
	}
	f.lines = lines
	// Normalize cursor/scroll to keep them within bounds of the new
	// content.
	if f.HasCursor {
		f.EnsureCursorVisible()
	} else {
		// For frames without a cursor, keep the viewport at the top when
		// content changes.
		f.scroll = 0
	}
}

// Cursor returns the current logical cursor position within the
// frame's content buffer. Callers can use this to synchronize external
// state with the frame's notion of cursor location.
func (f *Frame) Cursor() (line, col int) {
	return f.cursorLine, f.cursorCol
}

// NewFrame creates a new Frame backed by a goncurses subwindow. The
// window is created with the given height and width, positioned at
// (startY, startX) relative to the parent window. If hasBorder is true
// the frame draws a simple box border and reduces the inner content
// area accordingly.
//
// The hasCursor flag enables placing the terminal cursor within the
// frame during Render calls; hasInput enables the internal multi-line
// text buffer and editing helpers.
func NewFrame(parent *gc.Window, height, width, startY, startX int, hasBorder, hasCursor, hasInput bool) (*Frame, error) {
	if parent == nil {
		return nil, fmt.Errorf("parent window is nil")
	}

	if height < 1 {
		height = 1
	}
	if width < 1 {
		width = 1
	}

	win, err := gc.NewWindow(height, width, startY, startX)
	if err != nil {
		return nil, err
	}
	_ = win.Keypad(true)

	f := &Frame{
		Win:       win,
		Height:    height,
		Width:     width,
		HasBorder: hasBorder,
		HasCursor: hasCursor,
		HasInput:  hasInput,
		WrapMode:  WrapModeHard,
	}
	// Wrap the frame's window in a panel so that it participates in the
	// global panel stack. This allows callers to hide/delete the top
	// panel (such as a modal dialog) and then call UpdatePanels() and a
	// single Refresh() on the root window to restore the correct
	// composition of all remaining windows.
	f.pan = gc.NewPanel(win)

	if hasInput {
		f.ResetInput()
	}

	return f, nil
}

// SetWrapMode sets the frame-level wrap mode.
func (f *Frame) SetWrapMode(mode WrapMode) {
	f.WrapMode = mode
}

// Close deletes the underlying ncurses window.
func (f *Frame) Close() {
	if f == nil || f.Win == nil {
		return
	}
	// If this frame participates in the panel stack, remove its panel
	// first so that update_panels() will no longer consider it.
	if f.pan != nil {
		_ = f.pan.Delete()
		f.pan = nil
	}
	_ = f.Win.Delete()
}

// contentBounds returns the top-left coordinate and dimensions of the
// inner content area, excluding any border. The height and width values
// are guaranteed to be at least 1.
func (f *Frame) contentBounds() (y, x, h, w int) {
	y, x = 0, 0
	h, w = f.Height, f.Width
	if f.HasBorder {
		y++
		x++
		h -= 2
		w -= 2
	}
	if h < 1 {
		h = 1
	}
	if w < 1 {
		w = 1
	}
	return
}

// Render draws the frame border (if any), the current content lines
// held in the frame's buffer, the vertical scrollbar (when needed),
// and places the terminal cursor when HasCursor is true and showCursor
// is true.
//
// Callers are expected to populate the frame's content via SetLines for
// read-only views (such as history panes) or via the input helpers
// (ResetInput, InsertRune, etc.) for editable views.
func (f *Frame) Render(showCursor bool) {
	if f == nil || f.Win == nil {
		return
	}

	f.Win.Erase()

	if f.HasBorder {
		_ = f.Win.Box(0, 0)
	}

	contentY, contentX, contentH, contentW := f.contentBounds()
	if contentH <= 0 || contentW <= 0 {
		return
	}

	source := f.lines
	if len(source) == 0 {
		return
	}

	// Frames currently treat HasInput buffers as a distinct UI surface; we keep
	// them hard-wrapped regardless of the configured WrapMode.
	if f.HasInput {
		f.WrapMode = WrapModeHard
	}

	visibleHeight := contentH
	// Always reserve last column of the content area for the scrollbar.
	textWidth := contentW - 1
	if textWidth < 1 {
		textWidth = 1
	}

	// Expand the logical lines into display lines using the configured wrap
	// mode.
	//
	// NOTE: This is done at render time so that editable frames (input
	// buffers) can flow visually without mutating their underlying logical
	// buffer.
	display := f.buildDisplayLines(textWidth)

	if len(display) == 0 {
		return
	}

	// Determine effective scroll offset (in display lines).
	offset := f.scroll
	if offset < 0 {
		offset = 0
	}
	if offset > len(display) {
		offset = len(display)
	}

	// Render visible display lines.
	for row := 0; row < visibleHeight; row++ {
		idx := offset + row
		if idx >= len(display) {
			break
		}
		dl := display[idx]
		_ = f.Win.AttrSet(dl.attr)

		textRunes := dl.runes
		if dl.showMarker {
			if textWidth == 1 {
				textRunes = []rune{'\\'}
			} else {
				textRunes = append(append([]rune{}, textRunes...), '\\')
			}
		}
		if len(textRunes) > textWidth {
			// In WrapModeOff, dl.runes may contain the full logical line and must be
			// truncated here. In other modes this is a safety clamp.
			textRunes = textRunes[:textWidth]
		}
		f.Win.MovePrint(contentY+row, contentX, string(textRunes))
	}

	// Draw scrollbar in the last column of the content area.
	DrawScrollbarColumn(f.Win, contentY, visibleHeight, contentX+contentW-1,
		len(display), offset)

	// Terminal cursor placement.
	//
	// Because this Frame is backed by its own ncurses Window (newwin), we
	// need to move the cursor relative to the root screen so that the
	// terminal cursor appears in the correct place.
	if f.HasCursor && showCursor {
		// Map logical cursor position to display coordinates.
		cursorY := -1
		cursorX := 0

		// Identify which display line contains the logical cursor.
		for di := 0; di < len(display); di++ {
			dl := display[di]
			if dl.logicalIdx != f.cursorLine {
				continue
			}
			// Cursor can be at len(line) (end-of-line). Treat that as being
			// within the last segment for that line.
			col := f.cursorCol
			if col < dl.startCol {
				continue
			}
			segEnd := dl.startCol + dl.segContentLen
			// Include end-of-segment when this is the last segment for the line.
			isLastSeg := !dl.continues
			if col < segEnd || (isLastSeg && col == segEnd) {
				cursorY = di
				x := col - dl.startCol
				if x < 0 {
					x = 0
				}
				cursorX = dl.indentLen + x
				if cursorX < 0 {
					cursorX = 0
				}
				// Clamp to the last visible cell for this row. (Cursor mapping works in
				// logical coordinates, but the physical cursor cannot move past the
				// visible text width.)
				if cursorX > textWidth-1 {
					cursorX = textWidth - 1
				}
				break
			}
		}

		if cursorY == -1 {
			cursorY = 0
			cursorX = 0
		}

		cy := contentY + (cursorY - offset)
		if cy < contentY {
			cy = contentY
		}
		if cy >= contentY+visibleHeight {
			cy = contentY + visibleHeight - 1
		}
		cx := f.clampCursorX(cursorX, contentW)
		cx += contentX

		// Convert window-relative coordinates into screen-relative.
		wy, wx := f.Win.YX()
		yAbs := wy + cy
		xAbs := wx + cx
		gc.StdScr().Move(yAbs, xAbs)
	}

	// Do not flush to the physical terminal here. Higher-level code should
	// batch updates across multiple windows/panels and call goncurses.Update()
	// once.
}
