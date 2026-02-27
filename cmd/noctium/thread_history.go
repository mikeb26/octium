/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"strings"

	"github.com/mikeb26/octium/internal/threads"
	"github.com/mikeb26/octium/internal/types"
	"github.com/mikeb26/octium/internal/ui"
	"github.com/negrel/assert"
	gc "github.com/rthornton128/goncurses"
)

func historySourceLabel(src threads.RenderBlockSource) string {
	switch src {
	case threads.RenderBlockSourceUser:
		return "You:"
	case threads.RenderBlockSourceAssistant:
		return "LLM:"
	default:
		return ""
	}
}

// buildHistoryLines converts the logical RenderBlocks for a thread
// into a flat slice of ui.FrameLine values, inserting a standalone
// source label line ("You:" / "LLM:") when switching between
// user and assistant content, and then rendering the block text with no
// indentation. Lines are soft-wrapped with a trailing '\\' on wrapped
// segments. The resulting slice is suitable for direct line-by-line
// rendering in the history pane via a ui.Frame.
func buildHistoryLines(cliCtx *CliContext, blocks []threads.RenderBlock,
	width int) []ui.FrameLine {

	// We need at least two columns: one for text and one for the history
	// frame's scrollbar. Below that threshold we simply omit history
	// rendering.
	if width <= 1 {
		return nil
	}
	lines := make([]ui.FrameLine, 0)

	// The history frame reserves its last column for a scrollbar, so we
	// only have (width-1) columns available for text. Wrapping must obey
	// this limit or Frame.Render will apply an additional truncation
	// (including its own '\\' marker) and drop characters at wrap
	// boundaries.
	textWidth := width - 1
	if textWidth < 1 {
		textWidth = 1
	}

	var prevSource threads.RenderBlockSource
	havePrevSource := false
	for _, b := range blocks {
		showLabel := !havePrevSource || prevSource != b.Source
		baseStyle := gc.A_NORMAL
		baseColor := gc.A_NORMAL

		switch b.Source {
		case threads.RenderBlockSourceUser:
			baseStyle = gc.A_NORMAL
			baseColor = gc.A_NORMAL
		case threads.RenderBlockSourceAssistant:
			baseStyle = gc.A_NORMAL
			if cliCtx.toggles.useColors {
				baseColor = gc.ColorPair(threadColorAssistant)
			} else {
				baseStyle = gc.A_BOLD
				baseColor = gc.A_NORMAL
			}
		}

		attrBase := baseStyle | baseColor

		// Insert a blank line when switching between user/assistant sources.
		if showLabel {
			if havePrevSource {
				lines = append(lines, ui.FrameLine{Runes: []rune{}, Attr: gc.A_NORMAL})
			}
			label := historySourceLabel(b.Source)
			// The label line is styled based on its source (e.g. assistant color/bold),
			// but should not inherit code styling from the first block.
			lines = append(lines, ui.FrameLine{Runes: []rune(label), Attr: attrBase | gc.A_UNDERLINE})
		}

		attrText := attrBase
		if b.IsCode {
			attrText = baseStyle | ui.AttrItalic()
			if cliCtx.toggles.useColors {
				switch b.Source {
				case threads.RenderBlockSourceAssistant:
					attrText |= gc.ColorPair(threadColorAssistantCode)
					// On terminals that only support 8 colors, bold often maps to
					// "bright" versions of the base colors.
					if gc.Colors() < 16 {
						attrText |= gc.A_BOLD
					}
				case threads.RenderBlockSourceUser:
					attrText |= gc.ColorPair(threadColorUserCode)
					// If we only have 8 colors, fall back to dim white to approximate
					// grey.
					if gc.Colors() < 16 {
						attrText |= gc.A_DIM
					}
				default:
					attrText = attrBase | ui.AttrItalic()
				}
			} else {
				attrText = attrBase | ui.AttrItalic()
			}
		}

		// Thread history uses a Frame with render-time wrapping now, so we do not
		// pre-wrap here. We split only on explicit newlines to preserve the
		// original logical line structure.
		//
		// We rely on per-line WrapMode overrides to keep code blocks hard-wrapped
		// even when the global mode is word/off.
		wrapMode := ui.WrapModeInherit
		if b.IsCode {
			wrapMode = ui.WrapModeHard
		}
		parts := strings.Split(b.Text, "\n")
		if len(parts) == 0 {
			parts = []string{""}
		}
		for _, part := range parts {
			lines = append(lines, ui.FrameLine{Runes: []rune(part), Attr: attrText, WrapMode: wrapMode})
		}
		prevSource = b.Source
		havePrevSource = true
	}

	// If the thread is empty, ensure the history frame still has a logical line
	// so the cursor can be displayed when the history pane is focused.
	if len(lines) == 0 {
		lines = []ui.FrameLine{{Runes: []rune{}, Attr: gc.A_NORMAL}}
	}

	return lines
}

func buildHistoryLinesForThread(cliCtx *CliContext, thread threads.Thread,
	width int) []ui.FrameLine {

	return buildHistoryLines(cliCtx, thread.RenderBlocks(), width)
}

func (tvUI *threadViewUI) setHistoryFrameFromBlocks(
	blocks []threads.RenderBlock,
	extraAssistantText string,
	follow bool,
) {
	assert.NotNil(tvUI.historyFrame, "nil history frame setting history")

	cursorLine, cursorCol := tvUI.historyFrame.Cursor()

	fullBlocks := append([]threads.RenderBlock(nil), blocks...)
	if extraAssistantText != "" {
		extraBlocks := threads.RenderBlocksFromDialogue([]*types.ThreadMessage{{
			Role:    types.LlmRoleAssistant,
			Content: extraAssistantText,
		}})
		fullBlocks = append(fullBlocks, extraBlocks...)
	}
	_, maxX := tvUI.cliCtx.rootWin.MaxYX()
	lines := buildHistoryLines(tvUI.cliCtx, fullBlocks, maxX)
	tvUI.historyFrame.SetLines(lines)
	if follow {
		tvUI.historyFrame.MoveEnd()
		return
	}

	// Restore cursor position best-effort.
	// Clamp by letting EnsureCursorVisible fix bounds.
	tvUI.historyFrame.MoveHome()
	for i := 0; i < cursorLine; i++ {
		tvUI.historyFrame.MoveCursorDown()
	}
	for i := 0; i < cursorCol; i++ {
		tvUI.historyFrame.MoveCursorRight()
	}
	tvUI.historyFrame.EnsureCursorVisible()
}

func (tvUI *threadViewUI) setHistoryFrameForThread() {
	_, maxX := tvUI.cliCtx.rootWin.MaxYX()
	tvUI.historyFrame.SetLines(buildHistoryLinesForThread(tvUI.cliCtx, tvUI.thread, maxX))
	tvUI.historyFrame.MoveEnd()
}

// syncHistoryFrameWithCurrentThreadState ensures the history frame reflects the
// best available state.
//
// If the thread has an in-flight ChatOnceAsync, the user's pending prompt and
// any partial assistant output are not yet persisted into the thread's stored
// dialogue. In that case we must build history from the persisted blocks plus
// the running state's prompt/content.
func (tvUI *threadViewUI) syncHistoryFrameWithCurrentThreadState() {
	if tvUI.running.state != nil {
		state := tvUI.running.state
		blocks := threadViewDisplayBlocks(tvUI.thread, state.Prompt)
		content := state.ContentSoFar()
		tvUI.setHistoryFrameFromBlocks(blocks, content, tvUI.running.followHistory)
		// Keep async refresh bookkeeping consistent with what we've rendered.
		tvUI.running.lastContentLen = len(content)
		return
	}

	tvUI.setHistoryFrameForThread()
}

func threadViewDisplayBlocks(thread threads.Thread, pendingPrompt string) []threads.RenderBlock {
	blocks := append([]threads.RenderBlock(nil), thread.RenderBlocks()...)
	if pendingPrompt != "" {
		extraBlocks := threads.RenderBlocksFromDialogue([]*types.ThreadMessage{{
			Role:    types.LlmRoleUser,
			Content: pendingPrompt,
		}})
		blocks = append(blocks, extraBlocks...)
	}
	return blocks
}
