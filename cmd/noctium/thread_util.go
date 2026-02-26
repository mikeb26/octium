/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"context"
	"fmt"
	"math"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/threads"
	gc "github.com/rthornton128/goncurses"
)

func formatCount3SigDigits(n int) string {
	if n < 0 {
		return "-" + formatCount3SigDigits(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	suffixes := []string{"", "K", "M", "B", "T"}
	idx := 0
	scale := 1
	for idx+1 < len(suffixes) && n >= scale*1000 {
		scale *= 1000
		idx++
	}

	v := float64(n) / float64(scale)
	rounded, decimals := roundTo3SigDigits(v)
	if rounded >= 1000 && idx+1 < len(suffixes) {
		idx++
		scale *= 1000
		v = float64(n) / float64(scale)
		rounded, decimals = roundTo3SigDigits(v)
	}

	return fmt.Sprintf("%.*f%s", decimals, rounded, suffixes[idx])
}

func roundTo3SigDigits(v float64) (rounded float64, decimals int) {
	if v == 0 {
		return 0, 0
	}

	exp := int(math.Floor(math.Log10(math.Abs(v))))
	decimals = 2 - exp
	if decimals < 0 {
		decimals = 0
	}

	factor := math.Pow10(decimals)
	return math.Round(v*factor) / factor, decimals
}

// drawThreadHeader renders a single-line header for the thread view.
func (tvUI *threadViewUI) drawThreadHeader(ctx context.Context) {
	_ = tvUI.thread.Workspace().SyncSandbox(ctx, false) // best effort

	_, maxX := tvUI.cliCtx.rootWin.MaxYX()
	header := fmt.Sprintf("Thread: %s", tvUI.thread.Name())
	wsStatus := tvUI.thread.Workspace().String(ctx)
	if wsStatus != "" {
		header = fmt.Sprintf("%s | workspace: %s", header, wsStatus)
	}

	if len([]rune(header)) > maxX {
		header = string([]rune(header)[:maxX])
	}

	var attr gc.Char = gc.A_NORMAL
	if tvUI.cliCtx.toggles.useColors {
		attr |= gc.ColorPair(menuColorHeader)
	}
	_ = tvUI.cliCtx.rootWin.AttrSet(attr)
	for x := 0; x < maxX; x++ {
		tvUI.cliCtx.rootWin.MoveAddChar(0, x, gc.Char(' ')|attr)
	}
	_ = tvUI.cliCtx.rootWin.TouchLine(0, 1)
	tvUI.cliCtx.rootWin.MovePrint(0, 0, header)
	_ = tvUI.cliCtx.rootWin.AttrSet(gc.A_NORMAL)
}

// drawNavbar renders a simple status line at the bottom of the
// screen, including mode information and key hints.
func drawNavbar(cliCtx *CliContext, focus threadViewFocus, isArchived bool, thread threads.Thread) {
	maxY, maxX := cliCtx.rootWin.MaxYX()
	statusY := maxY - 1
	if statusY < 0 {
		return
	}

	segments := []statusSegment{
		{text: "Nav:", bold: false},
		{text: "↑", bold: true},
		{text: "/", bold: false},
		{text: "↓", bold: true},
		{text: "/", bold: false},
		{text: "→", bold: true},
		{text: "/", bold: false},
		{text: "←", bold: true},
		{text: "/", bold: false},
		{text: "PgUp", bold: true},
		{text: "/", bold: false},
		{text: "PgDn", bold: true},
		{text: "/", bold: false},
		{text: "Home", bold: true},
		{text: "/", bold: false},
		{text: "End", bold: true},
	}
	if !isArchived {
		segments = append(segments, []statusSegment{
			{text: " OtherWin:", bold: false},
			{text: "Tab", bold: true},
			{text: " Send:", bold: false},
			{text: "Ctrl-d", bold: true},
		}...)
	}
	if focus == focusHistory && !isArchived {
		segments = append(segments, []statusSegment{
			{text: " Workspace:", bold: false},
			{text: "w", bold: true},
			{text: " Rename:", bold: false},
			{text: "n", bold: true},
			// we intentionally do not display each workspace hotkey
			// for space savings. all are accessible under the workspace
			// menu
		}...)
	}
	segments = append(segments, []statusSegment{
		{text: " Back:", bold: false},
		{text: "ESC", bold: true},
	}...)

	// Draw the status segments first; additional metadata (tokens, version)
	// is rendered into the remaining right-side space when available.
	x := drawStatusSegments(cliCtx.rootWin, statusY, maxX, segments,
		cliCtx.toggles.useColors)

	progAndVer := internal.CliToolName + "-" + versionText
	tail := progAndVer
	if thread != nil {
		m := thread.Metrics()
		total := m.TokenUsage.PromptTokens + m.TokenUsage.CompletionTokens
		if total > 0 {
			tail = fmt.Sprintf("[tokens:{in:%s out:%s}] %s",
				formatCount3SigDigits(m.TokenUsage.PromptTokens),
				formatCount3SigDigits(m.TokenUsage.CompletionTokens),
				progAndVer)
		}
	}
	drawStatusTail(cliCtx.rootWin, statusY, maxX, x, cliCtx.toggles.useColors, tail)
}

func (tvUI *threadViewUI) getFocus() threadViewFocus {
	if tvUI.focusedFrame == tvUI.historyFrame {
		return focusHistory
	}
	return focusInput
}
