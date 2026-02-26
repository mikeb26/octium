/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mikeb26/octium/internal/prompts"
	"github.com/mikeb26/octium/internal/threads"
	"github.com/mikeb26/octium/internal/ui"
	gc "github.com/rthornton128/goncurses"
)

const (
	// Additional color pairs for the thread view. These are initialized
	// alongside the menu colors in initUI so they can be reused by any
	// ncurses-based views.
	threadColorAssistant     int16 = 5
	threadColorAssistantCode int16 = 6
	threadColorUserCode      int16 = 7
)

// threadViewFocus tracks which pane is currently active inside the
// thread view. This determines how keys are interpreted (e.g. whether
// 'q' quits the view or is inserted into the input buffer).
type threadViewFocus int

const (
	focusHistory threadViewFocus = iota
	focusInput
)

const AgentsMD = "AGENTS.md"

type threadViewUI struct {
	cliCtx       *CliContext
	thread       threads.Thread
	isArchived   bool
	running      threadViewAsyncChatState
	inputFrame   *ui.Frame
	historyFrame *ui.Frame
	focusedFrame *ui.Frame
	// inputDraft preserves any unsent user input across detach/reattach of the
	// thread view (e.g. ESC back to menu and then re-enter the same thread).
	inputDraft           string
	inputDraftCursorLine int
	inputDraftCursorCol  int
}

// automatically add AGENTS.md to the system prompt when present in the user's
// repository
func (tvUI *threadViewUI) getSystemPrompt() string {
	ws := tvUI.thread.Workspace()
	if ws == nil || ws.Origin() == "" {
		return prompts.SystemMsg
	}

	// best effort
	content, err := os.ReadFile(filepath.Join(ws.Origin(), AgentsMD))
	if err != nil {
		return prompts.SystemMsg
	}

	return fmt.Sprintf("%v\nThe user's repository contains an AGENTS.md with the following additional instructions:\n%v\n:",
		prompts.SystemMsg, string(content))
}

func lookupThreadViewUI(cliCtx *CliContext,
	thread threads.Thread) *threadViewUI {

	tid := thread.Id()
	if existing, ok := cliCtx.threadViews[tid]; ok && existing != nil {
		return existing
	}

	return nil
}

func lookupOrCreateThreadViewUI(ctx context.Context, cliCtx *CliContext,
	thread threads.Thread, isArchivedIn bool) *threadViewUI {

	tvUI := lookupThreadViewUI(cliCtx, thread)
	if tvUI != nil {
		// Thread view UIs are cached per-thread so we can preserve async state
		// between detach/reattach. However, archive/unarchive can move the thread
		// between groups (changing its scratch dir) and can also change whether the
		// thread should be treated as archived.
		//
		// Always refresh these derived fields from the menu entry we were launched
		// from so we don't get stuck in a stale "archived" state after unarchiving
		// from search results.
		tvUI.isArchived = isArchivedIn
		if tvUI.isArchived {
			// If the thread is archived, ensure we don't carry over any in-flight
			// async run state from when it was active.
			tvUI.clearRunningState()
		}
		tvUI.thread = thread
		return tvUI
	}

	tid := thread.Id()
	tvUI = &threadViewUI{
		cliCtx:     cliCtx,
		thread:     thread,
		isArchived: isArchivedIn,
	}
	tvUI.clearRunningState()
	cliCtx.threadViews[tid] = tvUI

	return tvUI
}

func (tvUI *threadViewUI) createThreadViewFrames() error {
	maxY, maxX := tvUI.cliCtx.rootWin.MaxYX()

	historyLines := buildHistoryLinesForThread(tvUI.cliCtx, tvUI.thread, maxX)
	// History frame occupies the region between the header and the input
	// label. It is read-only but uses the Frame's cursor/scroll helpers
	// for navigation.
	historyStartY := menuHeaderHeight
	historyEndY := maxY - menuStatusHeight - threadInputHeight
	if historyEndY <= historyStartY {
		historyEndY = historyStartY + 1
	}
	historyH := historyEndY - historyStartY
	if historyH < 1 {
		historyH = 1
	}
	historyW := maxX

	var err error
	tvUI.historyFrame, err = ui.NewFrame(tvUI.cliCtx.rootWin, historyH, historyW,
		historyStartY, 0, false, true, false)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCreatingHistoryFrame, err)
	}
	tvUI.historyFrame.SetWrapMode(tvUI.cliCtx.toggles.wrapMode)
	tvUI.historyFrame.SetLines(historyLines)
	// Start with cursor at end of history.
	tvUI.historyFrame.MoveEnd()

	// Create a Frame to manage the editable multi-line input buffer and
	// its cursor/scroll state. The frame's content area starts on the
	// first row below the input label and extends down to the status bar.
	inputHeight := threadInputHeight
	inputStartY := maxY - menuStatusHeight - inputHeight
	if inputStartY < menuHeaderHeight {
		inputStartY = menuHeaderHeight
	}
	// The label occupies one row; actual editable content lives below it.
	frameY := inputStartY + 1
	frameH := inputHeight - 1
	if frameH < 1 {
		frameH = 1
	}
	frameW := maxX

	tvUI.inputFrame, err = ui.NewFrame(tvUI.cliCtx.rootWin, frameH, frameW, frameY,
		0, false, true, true)
	if err != nil {
		tvUI.historyFrame.Close()
		tvUI.historyFrame = nil
		return fmt.Errorf("%w: %w", ErrCreatingInputFrame, err)
	}
	tvUI.inputFrame.ResetInput()

	return nil
}

func (tvUI *threadViewUI) handleThreadViewResize() (needRedraw bool, err error) {
	oldFocus := tvUI.getFocus()
	inputLine, inputCol := 0, 0
	inputContent := tvUI.inputFrame.InputString()
	inputLine, inputCol = tvUI.inputFrame.Cursor()
	// Keep draft state in sync so it survives a subsequent detach.
	tvUI.inputDraft = inputContent
	tvUI.inputDraftCursorLine = inputLine
	tvUI.inputDraftCursorCol = inputCol

	resizeScreen(tvUI.cliCtx.rootWin)

	tvUI.closeThreadViewFrames()

	err = tvUI.createThreadViewFrames()
	if err != nil {
		return false, err
	}
	if oldFocus == focusHistory {
		tvUI.focusedFrame = tvUI.historyFrame
	} else {
		tvUI.focusedFrame = tvUI.inputFrame
	}

	restoreInputFrameContent(tvUI.inputFrame, inputContent, inputLine, inputCol)

	tvUI.syncHistoryFrameWithCurrentThreadState()

	return true, nil
}

func (tvUI *threadViewUI) closeThreadViewFrames() {
	if tvUI.historyFrame != nil {
		tvUI.historyFrame.Close()
		tvUI.historyFrame = nil
	}
	if tvUI.inputFrame != nil {
		tvUI.inputFrame.Close()
		tvUI.inputFrame = nil
	}
	tvUI.focusedFrame = nil
}

func (tvUI *threadViewUI) redrawThreadView(ctx context.Context) {
	// Apply current wrap mode preference.
	if tvUI.historyFrame != nil {
		tvUI.historyFrame.SetWrapMode(tvUI.cliCtx.toggles.wrapMode)
	}
	tvUI.cliCtx.ui.SetTheme(ui.Theme{UseColors: tvUI.cliCtx.toggles.useColors, SelectedPair: menuColorSelected, WrapMode: tvUI.cliCtx.toggles.wrapMode})

	// Cursor visibility is global ncurses state. Modal dialogs (e.g. tool
	// approvals) may temporarily hide the cursor and not restore it, which can
	// leave the thread view without a visible caret after a run.
	//
	// Reassert our desired cursor visibility on every redraw.
	ui.SetCursorVisible(tvUI.focusedFrame != nil && tvUI.focusedFrame.HasCursor)

	// Render history and input frames first.
	//
	// NOTE: we intentionally do NOT erase stdscr/rootWin here.
	// The thread view is composed of multiple panel windows (history/input)
	// plus a few single-row regions drawn directly on stdscr (header, input
	// label, navbar). If we erase stdscr and then stage it after panels, it can
	// overwrite the panel windows in the virtual screen.
	//
	// Instead, we only repaint the specific stdscr rows we own (each helper
	// already fills its entire row), and we stage stdscr last so that the navbar
	// is the final thing painted.
	tvUI.historyFrame.Render(tvUI.getFocus() == focusHistory)
	tvUI.inputFrame.Render(tvUI.getFocus() == focusInput)

	// Stage panel windows into the virtual screen in correct z-order.
	gc.UpdatePanels()

	// Draw stdscr regions last, but preserve the current terminal cursor
	// position so we don't steal focus from whichever frame is active.
	curY, curX := gc.StdScr().CursorYX()
	tvUI.drawThreadHeader(ctx)
	drawThreadInputLabel(tvUI)
	drawNavbar(tvUI.cliCtx, tvUI.getFocus(), tvUI.isArchived)
	gc.StdScr().Move(curY, curX)

	// Stage stdscr last (so its cursor position wins), then flush once.
	tvUI.cliCtx.rootWin.NoutRefresh()
	_ = gc.Update()
}

func (tvUI *threadViewUI) processThreadViewKey(
	ctx context.Context,
	ch gc.Key,
) (exit bool, needRedraw bool) {
	if ch == gc.KEY_TAB {
		if tvUI.getFocus() == focusInput {
			tvUI.focusedFrame = tvUI.historyFrame
		} else if !tvUI.isArchived {
			tvUI.focusedFrame = tvUI.inputFrame
		}

		return false, true
	}

	isHistory := tvUI.getFocus() == focusHistory
	isRunning := tvUI.running.state != nil
	// Exit keys.
	if ch == gc.Key(27) { // ESC
		return true, false
	}

	// Navigation keys (shared by both history and input frames).
	switch ch {
	case gc.KEY_LEFT:
		if isRunning {
			if isHistory {
				tvUI.running.followHistory = false
			} else {
				tvUI.running.followReasoning = false
			}
		}
		tvUI.focusedFrame.MoveCursorLeft()
		return false, true
	case gc.KEY_RIGHT:
		if isRunning {
			if isHistory {
				tvUI.running.followHistory = false
			} else {
				tvUI.running.followReasoning = false
			}
		}
		tvUI.focusedFrame.MoveCursorRight()
		return false, true
	case gc.KEY_UP:
		if isRunning {
			if isHistory {
				tvUI.running.followHistory = false
			} else {
				tvUI.running.followReasoning = false
			}
		}
		tvUI.focusedFrame.MoveCursorUp()
		tvUI.focusedFrame.EnsureCursorVisible()
		return false, true
	case gc.KEY_DOWN:
		if isRunning {
			if isHistory {
				tvUI.running.followHistory = false
			} else {
				tvUI.running.followReasoning = false
			}
		}
		tvUI.focusedFrame.MoveCursorDown()
		tvUI.focusedFrame.EnsureCursorVisible()
		return false, true
	case gc.KEY_PAGEUP:
		if isRunning {
			if isHistory {
				tvUI.running.followHistory = false
			} else {
				tvUI.running.followReasoning = false
			}
		}
		tvUI.focusedFrame.ScrollPageUp()
		if isHistory {
			tvUI.focusedFrame.EnsureCursorVisible()
		}
		return false, true
	case gc.KEY_PAGEDOWN:
		if isRunning {
			if isHistory {
				tvUI.running.followHistory = false
			} else {
				tvUI.running.followReasoning = false
			}
		}
		tvUI.focusedFrame.ScrollPageDown()
		if isHistory {
			tvUI.focusedFrame.EnsureCursorVisible()
		}
		return false, true
	case gc.KEY_HOME:
		if isRunning {
			if isHistory {
				tvUI.running.followHistory = false
			} else {
				tvUI.running.followReasoning = false
			}
		}
		tvUI.focusedFrame.MoveHome()
		return false, true
	case gc.KEY_END:
		if isRunning {
			if isHistory {
				tvUI.running.followHistory = true
			} else {
				tvUI.running.followReasoning = true
			}
		}
		tvUI.focusedFrame.MoveEnd()
		return false, true
	case 'c':
		if isHistory {
			return false, tvUI.workspaceCommit(ctx)
		} // else do not return; inputFrame needs to process 'c' as input
	case 'd':
		if isHistory {
			return false, tvUI.workspaceDiff(ctx)
		} // else do not return; inputFrame needs to process 'd' as
	case 'm':
		if isHistory {
			if tvUI.ensureWorkspaceReady(ctx) {
				_ = workspaceMerge(ctx, tvUI)
			}
			return false, true
		}
	case 'p':
		if isHistory {
			if tvUI.ensureWorkspaceReady(ctx) {
				_ = workspacePush(ctx, tvUI)
			}
			return false, true
		}
	case 'r':
		if isHistory {
			if tvUI.ensureWorkspaceReady(ctx) {
				_ = workspaceReset(ctx, tvUI)
			}
			return false, true
		}
	case 's':
		if isHistory {
			if tvUI.ensureWorkspaceReady(ctx) {
				_ = workspaceSync(ctx, tvUI)
			}
			return false, true
		}
	case 't':
		if isHistory {
			if tvUI.ensureWorkspaceReady(ctx) {
				_ = tvUI.workspaceTerm(ctx)
			}
			return false, true
		}
	case 'w':
		if isHistory {
			_ = tvUI.launchWorkspaceModal(ctx)
			return false, true
		} // else do not return; inputFrame needs to process 'w' as input
	case 'd' - 'a' + 1: // Ctrl-D sends the input buffer
		if tvUI.isArchived {
			return false, false
		}
		prompt, ok := tvUI.beginAsyncChat(ctx)
		if ok {
			// Draft has been sent; clear preserved state so we don't restore it.
			tvUI.clearInputDraft()
			state := tvUI.running.state
			blocks := threadViewDisplayBlocks(tvUI.thread, prompt)
			tvUI.setHistoryFrameFromBlocks(blocks, state.ContentSoFar(), tvUI.running.followHistory)
			tvUI.setInputFrameFromReasoning(state.ReasoningSoFar(), tvUI.running.followReasoning)
			// Do not block waiting for completion; the UI loop will
			// continue processing async events and the user can detach.
		}
		return false, true
	}

	if isHistory {
		return false, false
	}

	// Input-only keys.
	switch ch {
	case gc.KEY_BACKSPACE, 127, 8:
		tvUI.inputFrame.Backspace()
		tvUI.inputFrame.EnsureCursorVisible()
		return false, true
	case gc.KEY_DC:
		// Delete key: remove the character under the cursor (forward delete).
		tvUI.inputFrame.DeleteForward()
		tvUI.inputFrame.EnsureCursorVisible()
		return false, true
	case gc.KEY_ENTER, gc.KEY_RETURN:
		tvUI.inputFrame.InsertNewline()
		tvUI.inputFrame.EnsureCursorVisible()
		return false, true
	default:
		// Treat any printable byte (including high‑bit bytes from
		// UTF‑8 sequences) as input. When running in a UTF-8
		// locale, ncurses/GetChar returns each byte of the sequence
		// separately; group those bytes into a single rune so that
		// characters like emoji render correctly.
		if ch >= 32 && ch < 256 {
			r := ui.ReadUTF8KeyRune(tvUI.cliCtx.rootWin, ch)
			tvUI.inputFrame.InsertRune(r)
			tvUI.inputFrame.EnsureCursorVisible()
			return false, true
		}
	}

	return false, false
}

// runThreadView provides an ncurses-based view for interacting with a
// single thread. It renders the existing dialogue and allows the user
// to enter a multi-line prompt in a 3-line input box. Ctrl-D sends the
// current input buffer via ChatOnceAsync. History and input
// areas are independently scrollable via focus switching (Tab) and
// standard navigation keys. Pressing 'q' or ESC in the history focus
// returns to the menu.
func runThreadView(ctx context.Context, cliCtx *CliContext,
	thread threads.Thread, isArchived bool) error {

	// Use the terminal cursor for caret display in the thread view.
	ui.SetCursorVisible(true)
	defer ui.SetCursorVisible(false)

	// Listen for SIGWINCH so we can adjust layout on resize while inside
	// the thread view. This mirrors the behavior of showMenu but keeps
	// all ncurses calls confined to this goroutine.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	tvUI := lookupOrCreateThreadViewUI(ctx, cliCtx, thread, isArchived)
	err := tvUI.createThreadViewFrames()
	if err != nil {
		return err
	}
	defer tvUI.closeThreadViewFrames()

	// Restore any in-progress draft input from a prior visit to this thread.
	tvUI.restoreInputDraft()

	// If we are re-entering a thread that has an in-flight async run, the
	// persisted thread dialogue won't include the pending user prompt yet.
	// Initialize history from the running state so the user sees their prompt
	// immediately even if the model hasn't streamed any new tokens.
	tvUI.syncHistoryFrameWithCurrentThreadState()

	tvUI.focusedFrame = tvUI.inputFrame
	if tvUI.isArchived {
		tvUI.focusedFrame = tvUI.historyFrame
	}

	// If this thread is currently running, keep the prompt input locked to the
	// state the run started with.
	if tvUI.running.state != nil {
		tvUI.clearInputDraft()
	}
	// Important: draw the thread view at least once before we service any
	// in-flight async approval requests. Otherwise, if the thread is currently
	// blocked awaiting approval and the approval request is already queued, the
	// approval modal can appear over the previous screen (the menu view).
	//
	// We still process async events immediately afterwards; this just ensures the
	// user sees the thread view first.
	tvUI.redrawThreadView(ctx)
	err = tvUI.setupWorkspace(ctx, false)
	if err != nil && !errors.Is(err, ErrWorkspaceNotConfigured) && !errors.Is(err, ErrWorkspaceSetupCancelled) {
		_ = tvUI.cliCtx.ui.Confirm(friendlyWorkspaceSetupErr(err))
	}
	err = nil
	needRedraw := true

	for {
		if runningNeedRedraw := tvUI.processAsyncChat(ctx); runningNeedRedraw {
			needRedraw = true
		}

		if needRedraw {
			tvUI.redrawThreadView(ctx)
			needRedraw = false
		}

		var ch gc.Key
		select {
		case <-sigCh:
			if resized, err := tvUI.handleThreadViewResize(); err != nil {
				return err
			} else if resized {
				needRedraw = true
			}
			continue
		default:
			ch = cliCtx.rootWin.GetChar()
			if ch == 0 {
				continue
			}
		}

		if ch == gc.KEY_RESIZE {
			if resized, err := tvUI.handleThreadViewResize(); err != nil {
				return err
			} else if resized {
				needRedraw = true
			}
			continue
		}

		exit, keyRedraw := tvUI.processThreadViewKey(ctx, ch)
		if exit {
			tvUI.captureInputDraft()
			tvUI.thread.Access()
			return nil
		}
		if keyRedraw {
			needRedraw = true
		}
	}
}
