/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mikeb26/octium/internal/prompts"
	"github.com/mikeb26/octium/internal/scm"
	"github.com/mikeb26/octium/internal/types"
)

const gitCommitBodyWrapWidth = 72

var reNewlines = regexp.MustCompile(`\r\n|\r|\n`)

const octiumAgentCoAuthorTrailer = "Co-authored-by: Octium Agent <agent@octium.dev>"

func (tvUI *threadViewUI) workspaceCommit(ctx context.Context) (needRedraw bool) {
	ws := tvUI.thread.Workspace()
	if ws.Sandbox() == "" {
		return false
	}

	opts := scm.CommitOptions{}
	opts.Message = tvUI.getCommitMessage(ctx)

	for {
		// This uses the user's configured git editor (git commit without -m).
		// Suspend curses so the editor can use the terminal.
		suspendNCurses()
		untracked, err := ws.CommitSandbox(ctx, opts)
		restoreNCurses()

		if err == nil {
			return true
		}

		if !errors.Is(err, scm.ErrUntrackedFiles) {
			_ = tvUI.cliCtx.ui.Confirm(err.Error())
			return true
		}

		// Ask whether to include each untracked file.
		if opts.IncludeUntracked == nil {
			opts.IncludeUntracked = make(map[string]bool)
		}
		for _, f := range untracked.Filename {
			// If already decided (e.g. retry), don't ask again.
			if _, ok := opts.IncludeUntracked[f]; ok {
				continue
			}

			prompt := fmt.Sprintf("Include currently untracked %v in this commit?", f)
			defaultNo := false
			include, selErr := tvUI.cliCtx.ui.SelectBool(
				prompt,
				types.UIOption{Key: "y", Label: "Yes, include"},
				types.UIOption{Key: "n", Label: "No, ignore"},
				&defaultNo,
			)
			if selErr != nil {
				_ = tvUI.cliCtx.ui.Confirm(selErr.Error())
				return true
			}
			opts.IncludeUntracked[f] = include
		}
		// Retry.
	}
}

// formatGitCommitMessage normalizes and word-wraps a commit message returned
// by an LLM.
//
// Conventions:
//   - Subject line is left as-is (trimmed) and not wrapped.
//   - The body is wrapped to gitCommitBodyWrapWidth columns.
//   - Existing blank lines are preserved.
//   - Bullet/numbered lines are left unwrapped to avoid mangling formatting.
func formatGitCommitMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}

	// Normalize newlines so we can safely split.
	msg = reNewlines.ReplaceAllString(msg, "\n")

	parts := strings.Split(msg, "\n")
	// Drop leading/trailing empty lines after normalization.
	for len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	for len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return ""
	}

	subject := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return subject
	}

	bodyLines := normalizeCommitBodyLines(parts[1:])
	if len(bodyLines) == 0 {
		return subject
	}
	return subject + "\n" + strings.Join(bodyLines, "\n")
}

func normalizeCommitBodyLines(lines []string) []string {
	// Trim any leading empty lines so we can enforce the common
	// "subject\n\nbody" convention.
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return nil
	}

	out := make([]string, 0, len(lines)+1)
	// Always keep exactly one blank line between subject and body.
	out = append(out, "")

	for i := 0; i < len(lines); {
		// Preserve blank lines.
		if strings.TrimSpace(lines[i]) == "" {
			out = append(out, "")
			i++
			continue
		}

		// Preserve bullet/numbered lines as-is (trimmed), as wrapping tends to
		// produce odd formatting.
		if isCommitListLine(lines[i]) {
			out = append(out, strings.TrimRight(lines[i], " \t"))
			i++
			continue
		}

		// Wrap a paragraph: consume contiguous non-empty, non-list lines.
		para := make([]string, 0, 4)
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !isCommitListLine(lines[i]) {
			para = append(para, strings.TrimSpace(lines[i]))
			i++
		}
		wrapped := wrapCommitParagraph(para, gitCommitBodyWrapWidth)
		out = append(out, wrapped...)
	}

	// Drop trailing blank lines.
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func isCommitListLine(line string) bool {
	trim := strings.TrimLeft(line, " \t")
	if trim == "" {
		return false
	}
	// Typical bullet list markers.
	if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") {
		return true
	}
	// Very small heuristic for numbered lists like "1. "
	if len(trim) >= 3 {
		// single digit + ". "
		if trim[0] >= '0' && trim[0] <= '9' && trim[1] == '.' && trim[2] == ' ' {
			return true
		}
	}
	return false
}

func wrapCommitParagraph(lines []string, width int) []string {
	if width < 1 {
		width = 1
	}
	if len(lines) == 0 {
		return nil
	}

	words := make([]string, 0, 32)
	for _, ln := range lines {
		fields := strings.Fields(ln)
		if len(fields) == 0 {
			continue
		}
		words = append(words, fields...)
	}
	if len(words) == 0 {
		return nil
	}

	out := make([]string, 0, 4)
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}

		if len([]rune(cur))+1+len([]rune(w)) <= width {
			cur += " " + w
			continue
		}

		out = append(out, cur)
		cur = w
	}
	if strings.TrimSpace(cur) != "" {
		out = append(out, cur)
	}
	return out
}

func (tvUI *threadViewUI) getCommitMessage(ctx context.Context) string {
	// use tvUI.cliCtx.llmClient.CreateChatCompletion(), tvUI.thread.Dialogue(),
	// and prompts.GitSummarizeMsg
	// else fall back to just "work in progress commit"
	const fallback = "work in progress commit"

	dialogue := tvUI.thread.Dialogue()
	filtered := make([]*types.ThreadMessage, 0, len(dialogue)+1)
	for _, msg := range dialogue {
		if msg == nil {
			continue
		}
		// Backwards compatibility: old threads may have persisted the system
		// message in the dialogue.
		if msg.Role == types.LlmRoleSystem {
			continue
		}
		filtered = append(filtered, msg)
	}

	req := make([]*types.ThreadMessage, 0, len(filtered)+1)
	req = append(req, &types.ThreadMessage{Role: types.LlmRoleSystem,
		Content: prompts.GitSummarizeMsg})
	req = append(req, filtered...)

	msg, err := tvUI.cliCtx.llmClient.CreateChatCompletion(ctx, req)
	if err != nil || msg == nil {
		return fallback
	}

	commitMsg := strings.TrimSpace(msg.Content)
	if commitMsg == "" {
		return fallback
	}

	commitMsg = formatGitCommitMessage(commitMsg)
	if strings.TrimSpace(commitMsg) == "" {
		return fallback
	}

	commitMsg = ensureCommitTrailer(commitMsg, octiumAgentCoAuthorTrailer)

	return commitMsg
}

func ensureCommitTrailer(commitMsg string, trailerLine string) string {
	commitMsg = reNewlines.ReplaceAllString(commitMsg, "\n")
	trailerLine = strings.TrimSpace(trailerLine)
	if trailerLine == "" {
		return commitMsg
	}

	lines := strings.Split(commitMsg, "\n")
	for _, ln := range lines {
		if strings.TrimSpace(ln) == trailerLine {
			return commitMsg
		}
	}

	// Ensure there is a blank line between the end of the commit description and
	// the trailer, per git/GitHub conventions.
	commitMsg = strings.TrimRight(commitMsg, "\n")
	for strings.HasSuffix(commitMsg, "\n\n\n") {
		commitMsg = strings.TrimRight(commitMsg, "\n")
	}
	return commitMsg + "\n\n" + trailerLine
}
