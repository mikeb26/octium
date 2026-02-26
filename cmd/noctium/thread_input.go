/* Copyright © 2025 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"strings"
	"time"

	"github.com/mikeb26/octium/internal/ui"
	"github.com/negrel/assert"
	gc "github.com/rthornton128/goncurses"
)

const (
	// Height (in rows) of the multi-line input box in the thread view.
	// This sits directly above the status bar.
	threadInputHeight = 6
)

// drawThreadInputLabel renders the separator / label row that visually
// separates the history pane from the input area. The editable content
// for the input area itself is now managed by an internal/ui.Frame
// instance owned by runThreadView.
func (tvUI *threadViewUI) threadInputLabelText(now time.Time) string {
	if tvUI == nil {
		return ""
	}

	if tvUI.running.state != nil {
		return tvUI.running.formatStatus(now)
	}

	if tvUI.isArchived {
		return asyncStatusArchived
	}

	return asyncStatusIdle
}

func (tvUI *threadViewUI) threadInputLabelTexts(now time.Time) (prefix, suffix string) {
	if tvUI == nil {
		return "", ""
	}

	if tvUI.running.state != nil {
		return tvUI.running.formatStatusPrefix(now), tvUI.running.formatStatusSuffix(now)
	}

	if tvUI.isArchived {
		return asyncStatusArchived, ""
	}

	return asyncStatusIdle, ""
}

func drawThreadInputLabel(tvUI *threadViewUI) {
	if tvUI == nil {
		return
	}

	cliCtx := tvUI.cliCtx
	prefixText, suffixText := tvUI.threadInputLabelTexts(time.Now())
	maxY, maxX := cliCtx.rootWin.MaxYX()
	inputHeight := threadInputHeight
	startY := maxY - menuStatusHeight - inputHeight
	if startY < menuHeaderHeight {
		startY = menuHeaderHeight
	}

	var sepAttr gc.Char = gc.A_NORMAL
	if cliCtx.toggles.useColors {
		sepAttr = gc.ColorPair(menuColorStatus)
	}
	_ = cliCtx.rootWin.AttrSet(sepAttr)

	// Reserve the last 2 cells for the terminating 'O₂' so the label row always
	// has a visual end-cap.
	endX := maxX - 2
	if endX < 0 {
		endX = 0
	}

	// NOTE:
	// - We intentionally avoid mvwhline()/HLine here. Even when embedding
	//   attributes into the chtype, some terminals/curses combos still do not
	//   consistently repaint the full row during incremental refreshes, which
	//   can make the status background look "truncated".
	// - Writing each cell explicitly ensures the full row is touched and uses
	//   the desired background attributes.
	// Fill the row (excluding the end-cap) with spaces so the background
	// attribute is applied consistently.
	for x := 0; x < endX; x++ {
		cliCtx.rootWin.MoveAddChar(startY, x, gc.Char(' ')|sepAttr)
	}

	// Right-justify the status suffix so it ends immediately before the 'O₂'
	// end-cap.
	prefixRunes := []rune(prefixText)
	suffixRunes := []rune(suffixText)
	widthBeforeCap := endX
	if widthBeforeCap < 0 {
		widthBeforeCap = 0
	}
	if len(suffixRunes) > widthBeforeCap {
		// Keep the tail of the suffix so the most relevant counters remain visible.
		suffixRunes = suffixRunes[len(suffixRunes)-widthBeforeCap:]
	}
	suffixStart := endX - len(suffixRunes)
	if suffixStart < 0 {
		suffixStart = 0
	}

	if len(prefixRunes) > suffixStart {
		prefixRunes = prefixRunes[:suffixStart]
	}
	if len(prefixRunes) > 0 {
		cliCtx.rootWin.MovePrint(startY, 0, string(prefixRunes))
	}
	if len(suffixRunes) > 0 && suffixStart < endX {
		cliCtx.rootWin.MovePrint(startY, suffixStart, string(suffixRunes))
	}
	if maxX > 1 {
		cliCtx.rootWin.MovePrint(startY, endX, "O₂")
	}
	_ = cliCtx.rootWin.TouchLine(startY, 1)
	_ = cliCtx.rootWin.AttrSet(gc.A_NORMAL)
}

func restoreInputFrameContent(inputFrame *ui.Frame, content string, cursorLine, cursorCol int) {
	if inputFrame == nil {
		return
	}
	inputFrame.ResetInput()
	for _, r := range []rune(content) {
		if r == '\n' {
			inputFrame.InsertNewline()
			continue
		}
		inputFrame.InsertRune(r)
	}

	// Restore cursor position best-effort.
	inputFrame.MoveHome()
	for i := 0; i < cursorLine; i++ {
		inputFrame.MoveCursorDown()
	}
	for i := 0; i < cursorCol; i++ {
		inputFrame.MoveCursorRight()
	}
	inputFrame.EnsureCursorVisible()
}

func (tvUI *threadViewUI) captureInputDraft() {
	assert.NotNil(tvUI.inputFrame)

	tvUI.inputDraft = tvUI.inputFrame.InputString()
	tvUI.inputDraftCursorLine, tvUI.inputDraftCursorCol = tvUI.inputFrame.Cursor()
}

func (tvUI *threadViewUI) restoreInputDraft() {
	assert.NotNil(tvUI.inputFrame)

	if tvUI.isArchived {
		// Keep the draft state cached so it can reappear if/when the thread is
		// later unarchived, but don't show it in a read-only view.
		return
	}
	restoreInputFrameContent(tvUI.inputFrame, tvUI.inputDraft,
		tvUI.inputDraftCursorLine, tvUI.inputDraftCursorCol)
}

func (tvUI *threadViewUI) clearInputDraft() {
	tvUI.inputDraft = ""
	tvUI.inputDraftCursorLine = 0
	tvUI.inputDraftCursorCol = 0
}

// setInputFrameFromReasoning replaces the input frame's buffer with the current
// reasoning content. This is used while a thread is running to show streamed
// reasoning output in the input pane.
//
// When follow is true, the cursor/viewport is pinned to the end as new content
// arrives. When false, the caller's current cursor/scroll position is preserved
// best-effort so the user can scroll through prior reasoning.
func (tvUI *threadViewUI) setInputFrameFromReasoning(reasoning string, follow bool) {
	assert.NotNil(tvUI.inputFrame, "null input frame when setting for reasoning")

	cursorLine, cursorCol := tvUI.inputFrame.Cursor()

	parts := strings.Split(reasoning, "\n")
	if len(parts) == 0 {
		parts = []string{""}
	}
	lines := make([]ui.FrameLine, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, ui.FrameLine{Runes: []rune(part), Attr: gc.A_NORMAL})
	}
	// Replace the frame content. We intentionally do not use ResetInput here so
	// that the frame behaves like a read-only scrollable pane while running.
	tvUI.inputFrame.SetLines(lines)

	if follow {
		tvUI.inputFrame.MoveEnd()
		return
	}

	// Restore prior cursor position best-effort.
	tvUI.inputFrame.MoveHome()
	for i := 0; i < cursorLine; i++ {
		tvUI.inputFrame.MoveCursorDown()
	}
	for i := 0; i < cursorCol; i++ {
		tvUI.inputFrame.MoveCursorRight()
	}
	tvUI.inputFrame.EnsureCursorVisible()
}
