/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/threads"
	"github.com/mikeb26/octium/internal/types"
	"github.com/mikeb26/octium/internal/ui"
	gc "github.com/rthornton128/goncurses"
	"golang.org/x/term"
)

const (
	RowFmt    = "│ %8v │ %8v │ %18v │ %18v │ %18v │ %-18v\n"
	RowSpacer = "──────────────────────────────────────────────────────────────────────────────────────────────\n"
)

func threadHeaderString(t threads.Thread) string {
	return threadHeaderStringWithState(t, t.State().String(), false)
}

func threadHeaderStringForMenu(t threads.Thread, isArchived bool) string {
	state := t.State().String()
	if isArchived {
		state = "archived"
	}
	return threadHeaderStringWithState(t, state, isArchived)
}

func threadHeaderStringWithState(t threads.Thread, state string, isArchived bool) string {
	now := time.Now()

	aTime := formatHeaderTime(t.AccessTime(), now)
	mTime := formatHeaderTime(t.ModTime(), now)
	cTime := formatHeaderTime(t.CreateTime(), now)

	// Append "*" when the thread needs user attention.
	//
	// 1) If the thread is blocked awaiting approval, we want the thread list
	//    (including the ncurses menu) to visually flag it immediately, even
	//    though no dialogue has been persisted yet.
	// 2) If the thread has been modified since it was last accessed, also flag it.
	stateSuffix := ""
	if !isArchived && (t.State() == threads.ThreadStateBlocked || t.ModTime().After(t.AccessTime())) {
		stateSuffix = "*"
	}

	return fmt.Sprintf(RowFmt, t.Id(), state+stateSuffix,
		aTime, mTime, cTime, t.Name())
}

// formatHeaderTime renders a timestamp for use in the thread list header.
// If the time falls on the same local calendar day as "now", the date
// portion is replaced with "Today". If it falls on the preceding
// calendar day, it is replaced with "Yesterday". Otherwise, the full
// date is shown. Calendar-day comparisons are done in the local time
// zone associated with "now" to avoid off-by-one errors around
// midnight or when using non-UTC locations.
func formatHeaderTime(ts time.Time, now time.Time) string {
	// Normalize the target time into the same location as "now" so
	// that calendar-day comparisons are meaningful.
	ts = ts.In(now.Location())

	full := ts.Format("01/02/2006 03:04pm")
	datePart := ts.Format("01/02/2006")

	y, m, d := now.Date()
	todayY, todayM, todayD := y, m, d
	yest := now.AddDate(0, 0, -1)
	yestY, yestM, yestD := yest.Date()
	ty, tm, td := ts.Date()

	switch {
	case ty == todayY && tm == todayM && td == todayD:
		return strings.Replace(full, datePart, "Today", 1)
	case ty == yestY && tm == yestM && td == yestD:
		return strings.Replace(full, datePart, "Yesterday", 1)
	default:
		return full
	}
}

func threadGroupHeaderString(includeSpacers bool) string {
	var sb strings.Builder

	if includeSpacers {
		sb.WriteString(RowSpacer)
	}
	sb.WriteString(fmt.Sprintf(RowFmt, "Thread#", "State", "Last Accessed",
		"Last Modified", "Created", "Name"))

	if includeSpacers {
		sb.WriteString(RowSpacer)
	}

	return sb.String()
}

func threadGroupFooterString() string {
	return RowSpacer
}

func threadGroupString(thrGrp *threads.ThreadGroup, header bool,
	footer bool) string {

	var sb strings.Builder

	if header {
		sb.WriteString(threadGroupHeaderString(true))
	}

	for _, t := range thrGrp.Threads() {
		sb.WriteString(threadHeaderString(t))
	}

	if footer {
		sb.WriteString(threadGroupFooterString())
	}

	return sb.String()
}

func (cliCtx *CliContext) isCurArchived() bool {
	return cliCtx.curThreadGroup == ArchiveThreadGroupName
}

func (cliCtx *CliContext) migrateOldThreadGroupFomatIfNeeded() error {
	oldMainDir, err := getThreadsDirOld()
	if err != nil {
		return err
	}
	oldArchiveDir, err := getArchiveDirOld()
	if err != nil {
		return err
	}
	thrGrpDir, err := getThreadGroupsDir()
	if err != nil {
		return err
	}

	err = cliCtx.migrateOneOldThreadGroupFormat(oldMainDir, thrGrpDir,
		MainThreadGroupName)
	if err != nil {
		return err
	}
	return cliCtx.migrateOneOldThreadGroupFormat(oldArchiveDir, thrGrpDir,
		ArchiveThreadGroupName)
}

func (cliCtx *CliContext) migrateOneOldThreadGroupFormat(oldDir string,
	thrGrpDir string, thrGrpName string) error {

	dEntries, err := os.ReadDir(oldDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, dEnt := range dEntries {
		err = cliCtx.migrateOneOldThreadFormat(dEnt, oldDir, thrGrpDir,
			thrGrpName)
		if err != nil {
			return err
		}
	}

	return os.RemoveAll(oldDir)
}

func (cliCtx *CliContext) migrateOneOldThreadFormat(dEntry os.DirEntry,
	oldDir string, thrGrpDir string, thrGrpName string) error {

	oldThreadFile := filepath.Join(oldDir, dEntry.Name())
	newThreadDir := strings.TrimSuffix(dEntry.Name(), path.Ext(dEntry.Name()))
	newThreadDir = filepath.Join(thrGrpDir, thrGrpName, newThreadDir)
	newThreadWorkDir := filepath.Join(newThreadDir, threads.ThreadScratchDir)
	newThreadFile := filepath.Join(newThreadDir, threads.ThreadFileName)

	content, err := os.ReadFile(oldThreadFile)
	if err != nil {
		return err
	}
	err = os.MkdirAll(newThreadWorkDir, 0700)
	if err != nil {
		return err
	}
	err = os.WriteFile(newThreadFile, content, 0600)
	if err != nil {
		return err
	}

	return nil
}

func (cliCtx *CliContext) migrateOldConfigDirIfNeeded() error {
	oldCDir, err := getConfigDirOld()
	if err != nil {
		return err
	}
	newCDir, err := getConfigDir()
	if err != nil {
		return err
	}

	f, err := os.Stat(oldCDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if !f.IsDir() {
		return fmt.Errorf("%w: %v", ErrConfigNotADir, oldCDir)
	}

	_, err = os.Stat(newCDir)
	if err == nil {
		return fmt.Errorf("%w: %v", ErrConfigExists, newCDir)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return os.Rename(oldCDir, newCDir)
}

func suspendNCurses() {
	gc.DefProgMode()
	gc.End()
}

func restoreNCurses() {
	gc.ResetProgMode()
	gc.UpdatePanels()
	gc.StdScr().Refresh()
}

// statusSegment represents a slice of text within a status bar and
// whether it should be highlighted as a key (bold / different color).
type statusSegment struct {
	text string
	bold bool
}

// drawStatusSegments renders a status bar composed of the provided
// segments on the given row. It applies a uniform background (reverse
// video or the menuColorStatus pair) and highlights bold segments using
// either A_BOLD or the menuColorStatusKey pair when colors are active.
func drawStatusSegments(scr *gc.Window, y, maxX int, segments []statusSegment, useColors bool) {
	var baseAttr gc.Char = gc.A_REVERSE
	if useColors {
		baseAttr = gc.ColorPair(menuColorStatus)
	}
	_ = scr.AttrSet(baseAttr)

	x := 0
	for _, seg := range segments {
		if x >= maxX {
			break
		}
		if seg.bold {
			if useColors {
				_ = scr.AttrSet(gc.ColorPair(menuColorStatusKey))
			} else {
				_ = scr.AttrOn(gc.A_BOLD)
			}
		} else {
			_ = scr.AttrSet(baseAttr)
		}

		remaining := maxX - x
		if remaining <= 0 {
			break
		}
		runes := []rune(seg.text)
		if len(runes) > remaining {
			runes = runes[:remaining]
		}
		text := string(runes)

		scr.MovePrint(y, x, text)
		x += len(runes)
	}

	// Ensure the remainder of the row keeps the base background attributes.
	//
	// Important: simply writing spaces (e.g. mvwaddch(' ')) is often optimized
	// away by ncurses as trailing blanks, which means the terminal never receives
	// any escape sequence to actually paint the background in those cells.
	//
	// Using clrtoeol after setting the desired attribute forces the remainder of
	// the line to be repainted/cleared using the current rendition, which keeps
	// the status bar background stable across incremental redraws.
	if x < maxX {
		ii := 0
		atLeastOnceSpace := false
		progAndVer := internal.CliToolName + "-" + versionText
		progStartX := maxX - len(progAndVer)
		_ = scr.AttrSet(baseAttr)
		for ; x < maxX; x++ {
			if atLeastOnceSpace && x >= progStartX {
				scr.MoveAddChar(y, x, gc.Char(progAndVer[ii])|baseAttr)
				ii++
			} else {
				scr.MoveAddChar(y, x, gc.Char(' ')|baseAttr)
				atLeastOnceSpace = true
			}
		}
	}
	_ = scr.TouchLine(y, 1)
}

// promptForThreadNameNCurses displays a simple centered modal window asking
// the user to enter a new thread name. It returns the entered string (with
// surrounding whitespace trimmed) or an empty string if the user cancels
// with ESC. All interaction happens via ncurses so it is safe to call while
// the main menu UI is active.
func promptForThreadNameNCurses(nui *ui.NcursesUI) (string, error) {
	// Delegate to the shared NcursesUI helper so we don't duplicate
	// modal input handling. ESC is treated as cancellation and mapped to
	// an empty string by NcursesUI.Get.
	name, err := nui.Get("Enter new thread name (ESC to cancel):")
	if err != nil {
		return "", err
	}

	return name, nil
}

// showErrorRetryModal displays a simple yes/no prompt using NcursesUI
// and returns true if the user chooses to retry. The prompt includes
// the error message followed by "Retry? (y/n)". ESC or an empty
// response are treated the same as selecting "n" (do not retry).
func showErrorRetryModal(nui *ui.NcursesUI, message string) (bool, error) {
	// Build a compact prompt that shows the error text and asks whether
	// to retry. NcursesUI.SelectBool handles rendering the modal and
	// collecting the response.
	prompt := fmt.Sprintf("Error: %s\n\nRetry sending the last prompt?\n\nNote: choosing 'No' will discard the last prompt (it will not be saved to the thread).", message)
	trueOpt := types.UIOption{Key: "y", Label: "Yes, retry"}
	falseOpt := types.UIOption{Key: "n", Label: "No, do not retry"}
	defaultNo := false

	return nui.SelectBool(prompt, trueOpt, falseOpt, &defaultNo)
}

// Helper to synchronize ncurses' idea of the terminal size with the
// actual TTY size. This uses golang.org/x/term to query the real
// dimensions, then asks ncurses (via goncurses) to resize its
// internal structures. All ncurses calls stay on this goroutine to
// avoid concurrency issues with C state.
func resizeScreen(scr *gc.Window) {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return
	}

	if !gc.IsTermResized(rows, cols) {
		return
	}

	_ = gc.ResizeTerm(rows, cols)
}
