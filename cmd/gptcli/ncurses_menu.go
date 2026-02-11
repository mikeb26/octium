/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/famz/SetLocale"
	"github.com/mikeb26/gptcli/internal/threads"
	"github.com/mikeb26/gptcli/internal/types"
	iui "github.com/mikeb26/gptcli/internal/ui"
	gc "github.com/rthornton128/goncurses"
	"golang.org/x/term"
)

func confirmQuitIfNonIdleThreads(gptCliCtx *CliContext) (bool, error) {
	nonIdle := gptCliCtx.threadGroupSet.NonIdleThreadCount()

	if nonIdle == 0 {
		return true, nil
	}

	prompt := fmt.Sprintf(
		"You have %d non-idle thread(s) (running or awaiting approval). Quit anyway?\nIf you quit now, you may lose progress/output.",
		nonIdle,
	)
	defaultQuit := false
	trueOpt := types.UIOption{Key: "y", Label: "Yes, quit anyway"}
	falseOpt := types.UIOption{Key: "n", Label: "No, I don't want to lose progress"}
	quit, err := gptCliCtx.ui.SelectBool(prompt, trueOpt, falseOpt, &defaultQuit)
	if err != nil {
		return false, err
	}

	return quit, nil
}

const (
	menuHeaderHeight         = 1
	menuStatusHeight         = 1
	menuColorHeader    int16 = 1
	menuColorStatus    int16 = 2
	menuColorSelected  int16 = 3
	menuColorStatusKey int16 = 4
)

func (ui *threadMenuUI) selectedEntry() *threadMenuEntry {
	if ui.selected < 0 || ui.selected >= len(ui.entries) {
		return nil
	}
	return &ui.entries[ui.selected]
}

func (ui *threadMenuUI) viewHeight() int {
	maxY, _ := ui.cliCtx.rootWin.MaxYX()
	vh := maxY - menuHeaderHeight - menuStatusHeight
	if vh < 0 {
		return 0
	}
	return vh
}

func (ui *threadMenuUI) adjustOffset() {
	vh := ui.viewHeight()
	total := len(ui.entries)
	iui.AdjustListViewport(total, vh, &ui.selected, &ui.offset)
}

func (ui *threadMenuUI) draw() {
	scr := ui.cliCtx.rootWin

	maxY, maxX := scr.MaxYX()
	vh := ui.viewHeight()
	showScrollbar := vh > 0 && len(ui.entries) > vh && maxX >= 2
	listWidth := maxX
	if showScrollbar {
		listWidth = maxX - 1
	}

	ui.adjustOffset()

	headerTitle := strings.Split(threadGroupHeaderString(false), "\n")[0]
	headerTitle = iui.TruncateRunes(headerTitle, maxX)

	if ui.cliCtx.toggles.useColors {
		_ = scr.AttrSet(gc.A_NORMAL | gc.ColorPair(menuColorHeader))
	} else {
		_ = scr.AttrSet(gc.A_NORMAL)
	}
	scr.Move(0, 0)
	scr.HLine(0, 0, ' ', maxX)
	scr.MovePrintf(0, 0, "%s", headerTitle)

	// Scrollable list area
	startY := menuHeaderHeight
	if vh > 0 {
		for row := 0; row < vh; row++ {
			idx := ui.offset + row
			rowY := startY + row

			isSelected := idx == ui.selected && idx < len(ui.entries)
			if isSelected {
				if ui.cliCtx.toggles.useColors {
					_ = scr.AttrSet(gc.A_NORMAL | gc.ColorPair(menuColorSelected))
				} else {
					_ = scr.AttrSet(gc.A_REVERSE | gc.A_NORMAL)
				}
			} else {
				_ = scr.AttrSet(gc.A_NORMAL)
			}

			// Fill the entire row so the background spans full width, then
			// render the visible text at the start of the line.
			scr.Move(rowY, 0)
			scr.HLine(rowY, 0, ' ', maxX)

			if idx >= len(ui.entries) {
				continue
			}
			line := iui.TruncateRunes(ui.entries[idx].label, listWidth)
			scr.MovePrintf(rowY, 0, "%s", line)
		}
	}

	// Draw scrollbar in the last column of the list area (only when needed).
	if showScrollbar {
		sb := iui.ComputeScrollbar(len(ui.entries), vh, ui.offset)
		for row := 0; row < vh; row++ {
			iui.DrawScrollbarCell(scr, startY+row, row, vh, maxX-1, sb)
		}
	}

	// Status bar at bottom
	_ = scr.AttrSet(gc.A_NORMAL)
	statusY := maxY - 1
	if statusY >= 0 {
		segments := ui.buildMenuStatusSegments(maxX)
		drawStatusSegments(scr, statusY, maxX, segments, ui.cliCtx.toggles.useColors)
	}

	_ = scr.AttrSet(gc.A_NORMAL)
	scr.Refresh()
}

func (ui *threadMenuUI) buildMenuStatusSegments(maxX int) []statusSegment {
	_ = maxX

	segments := []statusSegment{
		{text: "Nav:", bold: false},
		{text: "↑", bold: true},
		{text: "/", bold: false},
		{text: "↓", bold: true},
		{text: "/", bold: false},
		{text: "PgUp", bold: true},
		{text: "/", bold: false},
		{text: "PgDn", bold: true},
		{text: "/", bold: false},
		{text: "Home", bold: true},
		{text: "/", bold: false},
		{text: "End", bold: true},
		{text: " Select:", bold: false},
		{text: "⏎", bold: true},
	}
	if ent := ui.selectedEntry(); ent != nil {
		if !ent.isArchived {
			segments = append(segments, []statusSegment{
				{text: " Archive:", bold: false},
				{text: "a", bold: true},
			}...)
		} else {
			segments = append(segments, []statusSegment{
				{text: " Unarchive:", bold: false},
				{text: "u", bold: true},
			}...)
		}
	}

	if !ui.isSearchActive() {
		segments = append(segments, []statusSegment{
			{text: " New:", bold: false},
			{text: "n", bold: true},
			{text: " Search:", bold: false},
			{text: "/", bold: true},
		}...)
	}

	segments = append(segments, []statusSegment{
		{text: " Config:", bold: false},
		{text: "c", bold: true},
	}...)

	if !ui.isSearchActive() {
		segments = append(segments, []statusSegment{
			{text: " Quit:", bold: false},
			{text: "ESC", bold: true},
		}...)
	} else {
		segments = append(segments, []statusSegment{
			{text: " Exit Search:", bold: false},
			{text: "ESC", bold: true},
		}...)
	}

	return segments
}

func (ui *threadMenuUI) resetItems() {

	selectedThreadID := ui.selectedThreadID()

	if ui.isSearchActive() {
		ui.entries = ui.buildSearchEntries(ui.searchQuery)
	} else {
		items := make([]threadMenuEntry, 0)
		for _, t := range ui.cliCtx.threadGroupSet.Threads(
			[]string{ui.cliCtx.curThreadGroup}) {

			line := strings.TrimRight(threadHeaderString(t), "\n")
			entry := threadMenuEntry{
				label:      line,
				thread:     t,
				isArchived: ui.cliCtx.isCurArchived(),
			}
			items = append(items, entry)
		}
		ui.entries = items
	}

	ui.restoreSelection(selectedThreadID)

}

func (ui *threadMenuUI) selectedThreadID() string {
	if ui.selected < 0 || ui.selected >= len(ui.entries) {
		return ""
	}
	return ui.entries[ui.selected].thread.Id()
}

func (ui *threadMenuUI) restoreSelection(threadID string) {
	ui.selected = 0
	if len(ui.entries) == 0 || threadID == "" {
		return
	}

	for idx, ent := range ui.entries {
		if ent.thread.Id() == threadID {
			ui.selected = idx
			break
		}
	}
}

func gcInit() (*gc.Window, error) {
	// Require a real TTY; ncurses UI is not supported otherwise
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil, ErrTTYRequired
	}

	// Reduce ncurses' ESC-key delay so pressing ESC is responsive.
	//
	// In keypad mode, ncurses must disambiguate a literal ESC press from
	// an escape sequence (e.g. arrow keys), and it does so by waiting up
	// to ESCDELAY milliseconds for additional bytes.
	//
	// This MUST be set before initializing ncurses via gc.Init().
	_ = os.Setenv("ESCDELAY", "100")
	initLocaleForNCurses()
	rootWin, err := gc.Init()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToInitScreen, err)
	}

	return rootWin, nil
}

func initLocaleForNCurses() {
	// Enable UTF-8; must be set before initializing ncurses via gc.Init().
	//
	// We prefer the process environment (""), but fall back to C.UTF-8
	// because many minimal systems do not have en_US.UTF-8 generated.
	//
	// The vendored SetLocale wrapper returns "" if the locale isn't available.
	if loc := SetLocale.SetLocale(SetLocale.LC_ALL, ""); strings.Contains(strings.ToUpper(loc), "UTF-8") {
		return
	}
	if loc := SetLocale.SetLocale(SetLocale.LC_ALL, "C.UTF-8"); strings.Contains(strings.ToUpper(loc), "UTF-8") {
		return
	}
	if loc := SetLocale.SetLocale(SetLocale.LC_ALL, "C.utf8"); strings.Contains(strings.ToUpper(loc), "UTF-8") {
		return
	}
	_ = SetLocale.SetLocale(SetLocale.LC_ALL, "en_US.UTF-8")
}

func gcExit() {
	gc.End()
}

func showMenu(ctx context.Context, cliCtx *CliContext) error {
	// Listen for SIGWINCH (terminal resize). We handle the signal in this
	// same goroutine by polling the channel inside the UI loop, which
	// keeps all ncurses interaction single-threaded.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	cliCtx.initMenuUI()
	needErase := true
	needRefresh := false
	upgradeChecked := false

	// Keep internal/ui modal selection styling consistent with the menu's
	// colors (or fall back to reverse-video in monochrome mode).
	cliCtx.ui.SetTheme(iui.Theme{UseColors: cliCtx.toggles.useColors, SelectedPair: menuColorSelected})
	lastRefresh := time.Now()

	for {
		if needErase {
			cliCtx.rootWin.Erase()
			needErase = false
		}
		if needRefresh {
			cliCtx.menu.resetItems()
			needRefresh = false
			lastRefresh = time.Now()
		}

		cliCtx.menu.draw()

		var ch gc.Key
		select {
		case <-sigCh:
			resizeScreen(cliCtx.rootWin)
			needErase = true
			continue
		default:
			if !upgradeChecked {
				err := upgradeIfNeeded(ctx, cliCtx)
				if err == io.EOF {
					return err
				}
				upgradeChecked = true
			}
			if cliCtx.toggles.needConfig {
				configMain(ctx, cliCtx)
				needRefresh = true
				continue
			}

			ch = cliCtx.rootWin.GetChar()
			if ch == 0 {
				if time.Now().Sub(lastRefresh) > 200*time.Millisecond {
					needRefresh = true
				}
				continue
			}
		}

		switch ch {
		case gc.Key(27): // ESC
			if cliCtx.menu.isSearchActive() {
				cliCtx.menu.clearSearch()
				needRefresh = true
				continue
			}
			quit, err := confirmQuitIfNonIdleThreads(cliCtx)
			if err != nil {
				return err
			}
			if quit {
				return nil
			}
			needErase = true
			continue
		case gc.KEY_UP:
			if cliCtx.menu.selected > 0 {
				cliCtx.menu.selected--
			}
		case gc.KEY_DOWN:
			if cliCtx.menu.selected < len(cliCtx.menu.entries)-1 {
				cliCtx.menu.selected++
			}
		case gc.KEY_HOME:
			cliCtx.menu.selected = 0
		case gc.KEY_END:
			if len(cliCtx.menu.entries) > 0 {
				cliCtx.menu.selected = len(cliCtx.menu.entries) - 1
			}
		case gc.KEY_PAGEUP:
			if vh := cliCtx.menu.viewHeight(); vh > 0 {
				cliCtx.menu.selected -= vh
				if cliCtx.menu.selected < 0 {
					cliCtx.menu.selected = 0
				}
			}
		case gc.KEY_PAGEDOWN:
			if vh := cliCtx.menu.viewHeight(); vh > 0 {
				cliCtx.menu.selected += vh
				if cliCtx.menu.selected > len(cliCtx.menu.entries)-1 {
					cliCtx.menu.selected = len(cliCtx.menu.entries) - 1
				}
			}
		case gc.KEY_ENTER, gc.KEY_RETURN:
			// Enter: activate the selected thread and transition into the
			// ncurses-based thread view instead of dropping back to the
			// basic CLI prompt.
			if len(cliCtx.menu.entries) == 0 {
				continue
			}

			entry := cliCtx.menu.selectedEntry()
			err := entry.thread.Access()
			if err != nil {
				// Propagate the error so the caller can handle it and
				// exit ncurses cleanly.
				return err
			}
			if err := runThreadView(ctx, cliCtx, entry.thread, entry.isArchived); err != nil {
				return err
			}
			// After returning from the thread view, redraw the menu.
			needRefresh = true
			needErase = true
		case 'n', 'N':
			if cliCtx.menu.isSearchActive() {
				// Disabled while viewing search results.
				continue
			}
			needErase = true
			// Create a new thread (equivalent to the "new" subcommand), but
			// prompt for the name using an ncurses modal so we don't mix
			// stdio with the UI.
			name, err := promptForThreadNameNCurses(cliCtx.ui)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrFailedToPromptThreadName, err)
			}
			if name == "" { // user cancelled
				continue
			}
			err = cliCtx.threadGroupSet.NewThread(ctx, cliCtx.curThreadGroup,
				name)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrFailedToCreateThread, err)
			}
			needRefresh = true
		case 'c':
			configMain(ctx, cliCtx)
			needRefresh = true
		case '/':
			if cliCtx.menu.isSearchActive() {
				// Disabled while viewing search results.
				continue
			}
			needErase = true
			q, err := cliCtx.menu.promptForSearchQuery()
			if err != nil {
				return err
			}
			if q == "" {
				continue
			}
			cliCtx.menu.doSearch(q)
		case 'a', 'u':
			didMove, err := handleMenuArchiveUnarchive(ctx, cliCtx, ch)
			if err != nil {
				return err
			}
			if didMove {
				needErase = true
				needRefresh = true
			}
		case gc.KEY_RESIZE:
			resizeScreen(cliCtx.rootWin)
			needErase = true
			continue
		}
	}
}

func handleMenuArchiveUnarchive(ctx context.Context, cliCtx *CliContext, ch gc.Key) (bool, error) {
	entry := cliCtx.menu.selectedEntry()
	if entry == nil {
		return false, nil
	}
	if ch == 'a' && entry.isArchived {
		return false, nil
	}
	if ch == 'u' && !entry.isArchived {
		return false, nil
	}

	ok, err := confirmDiscardThreadWorkspaceIfDirtyOrAhead(ctx, cliCtx, entry.thread, entry.isArchived)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	dstThreadGroup := ArchiveThreadGroupName
	srcThreadGroup := MainThreadGroupName
	if entry.isArchived {
		dstThreadGroup = MainThreadGroupName
		srcThreadGroup = ArchiveThreadGroupName
	}

	err = cliCtx.threadGroupSet.MoveThread(entry.thread, srcThreadGroup, dstThreadGroup)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrFailedToArchiveThread, err)
	}

	entry.isArchived = !entry.isArchived
	return true, nil
}

func confirmDiscardThreadWorkspaceIfDirtyOrAhead(
	ctx context.Context,
	cliCtx *CliContext,
	thread threads.Thread,
	isArchived bool,
) (bool, error) {

	// Moving a thread between groups rewrites its persisted directory, which
	// does not preserve the thread's scratch contents (including any workspace
	// sandbox). If there is work in the sandbox, warn before proceeding.
	tvUI := lookupThreadViewUI(cliCtx, thread)
	if isArchived || tvUI == nil || tvUI.ws.Sandbox() == "" {
		return true, nil
	}

	st, err := tvUI.ws.SandboxSyncStatus(ctx)
	if err != nil {
		defaultNo := false
		prompt := fmt.Sprintf(
			"Could not determine workspace sandbox status (%v).\n\nArchiving may discard the thread's workspace sandbox. Continue anyway?",
			err,
		)
		return cliCtx.ui.SelectBool(
			prompt,
			types.UIOption{Key: "y", Label: "Yes, continue (discard workspace)"},
			types.UIOption{Key: "n", Label: "No, cancel"},
			&defaultNo,
		)
	}

	unsafe := st.HasUncommittedChanges || st.Ahead > 0
	if !unsafe {
		return true, nil
	}

	var reasons []string
	if st.HasUncommittedChanges {
		reasons = append(reasons, "- Sandbox has uncommitted changes")
	}
	if st.Ahead > 0 {
		up := "(no upstream configured)"
		if strings.TrimSpace(st.UpstreamRemote) != "" && strings.TrimSpace(st.UpstreamBranch) != "" {
			up = fmt.Sprintf("%v/%v", st.UpstreamRemote, st.UpstreamBranch)
		}
		reasons = append(reasons, fmt.Sprintf("- Sandbox is ahead of upstream %v by %d commit(s)", up, st.Ahead))
	}

	defaultNo := false
	prompt := fmt.Sprintf(
		"This thread's workspace sandbox appears to contain work that will be discarded by archiving/unarchiving:\n\n%v\n\nThrow away this work and continue?",
		strings.Join(reasons, "\n"),
	)
	ok, selErr := cliCtx.ui.SelectBool(
		prompt,
		types.UIOption{Key: "y", Label: "Yes, discard and continue"},
		types.UIOption{Key: "n", Label: "No, cancel"},
		&defaultNo,
	)
	if selErr != nil {
		return false, selErr
	}

	return ok, nil
}
