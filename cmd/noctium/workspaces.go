/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/scm"
	"github.com/mikeb26/octium/internal/threads"
	"github.com/mikeb26/octium/internal/types"
)

// launchWorkspaceModal opens a selection modal for workspace
// operations.
func (tvUI *threadViewUI) launchWorkspaceModal(ctx context.Context) error {
	// NUX: explain what a workspace is before we prompt for linking a repo.
	tvUI.cliCtx.nuxWorkspaceIntroIfNeeded()

	newlyCreated, ready := tvUI.ensureWorkspaceReady(ctx)
	if !ready || newlyCreated {
		return nil
	}

	choices := []types.UIOption{
		{Key: "d", Label: "Diff:d Show differences in the workspace sandbox"},
		{Key: "c", Label: "Commit:c Commit any uncommitted changes that may be present in the workspace sandbox"},
		{Key: "s", Label: "Sync:s Synchronize the workspace's sandbox from your repo"},
		{Key: "p", Label: "Push:p Push committed changes in the workspace's sandbox into a branch in your repo"},
		{Key: "m", Label: "Merge:m Merge committed changes in the workspace's sandbox into your repo's current branch"},
		{Key: "r", Label: "Reset:r Reset the workspace sandbox and/or change which of your repositories this thread works with"},
		{Key: "t", Label: "Terminal:t Open a terminal in the workspace sandbox"},
	}
	ws := tvUI.thread.Workspace()
	sel, err := tvUI.cliCtx.ui.SelectOption(ws.Detail(), choices)
	if err != nil {
		// cancel
		return err
	}

	switch sel.Key {
	case "d":
		_ = tvUI.workspaceDiff(ctx)
	case "c":
		_ = tvUI.workspaceCommit(ctx)
	case "t":
		err = tvUI.workspaceTerm(ctx)
	case "s":
		err = workspaceSync(ctx, tvUI)
	case "p":
		err = workspacePush(ctx, tvUI)
	case "r":
		err = workspaceReset(ctx, tvUI)
	case "m":
		err = workspaceMerge(ctx, tvUI)
	default:
		err = fmt.Errorf("unknown workspace option: %v", sel.Key)
	}

	return err
}

// ensureWorkspaceReady performs best-effort workspace setup before running
// workspace operations.
//
// It returns false if the user cancels setup or chooses not to configure a
// workspace right now.
func (tvUI *threadViewUI) ensureWorkspaceReady(ctx context.Context) (bool, bool) {
	ws := tvUI.thread.Workspace()
	if ws.Origin() != "" {
		return false, true
	}

	created, err := tvUI.setupWorkspace(ctx, true)
	if err == nil {
		return created, true
	}
	if errors.Is(err, ErrWorkspaceSetupCancelled) || errors.Is(err, ErrWorkspaceNotConfigured) {
		return false, false
	}

	_ = tvUI.cliCtx.ui.Confirm(friendlyWorkspaceSetupErr(err))
	return false, false
}

func workspaceSync(ctx context.Context, tvUI *threadViewUI) error {
	suspendNCurses()
	err := tvUI.thread.Workspace().SyncSandbox(ctx, true)
	restoreNCurses()
	if err != nil {
		_ = tvUI.cliCtx.ui.Confirm(err.Error())
	}

	return err
}

func workspacePush(ctx context.Context, tvUI *threadViewUI) error {
	// NUX: first time explanation.
	tvUI.cliCtx.nuxWorkspaceOpIntroIfNeeded(nuxKeyWorkspacePushIntro)

	dstBranch := branchNameFromThread(tvUI.thread)
	suspendNCurses()
	err := tvUI.thread.Workspace().PushSandbox(ctx, dstBranch)
	restoreNCurses()
	if err != nil {
		_ = tvUI.cliCtx.ui.Confirm(fmt.Sprintf("%v\nDestination branch: %v", err.Error(), dstBranch))
		return err
	}

	_ = tvUI.cliCtx.ui.Confirm(fmt.Sprintf("Successfully pushed to branch %v", dstBranch))
	return nil
}

func workspaceMerge(ctx context.Context, tvUI *threadViewUI) error {
	// NUX: first time explanation.
	tvUI.cliCtx.nuxWorkspaceOpIntroIfNeeded(nuxKeyWorkspaceMergeIntro)

	suspendNCurses()
	err := tvUI.thread.Workspace().MergeSandbox(ctx)
	restoreNCurses()
	if err != nil {
		_ = tvUI.cliCtx.ui.Confirm(err.Error())
	}

	return err
}

func branchNameFromThread(t threads.Thread) string {
	name := t.Name()

	// Use the first 16 characters of the thread name, replacing whitespace
	// with underscores.
	var sb strings.Builder
	runeCount := 0
	for _, r := range name {
		if runeCount >= 16 {
			break
		}
		if unicode.IsSpace(r) {
			r = '_'
		}
		sb.WriteRune(r)
		runeCount++
	}

	return fmt.Sprintf("%s/%s_%s", internal.CliToolName, sb.String(), t.Id())
}

func workspaceReset(ctx context.Context, tvUI *threadViewUI) error {
	// NUX: first time explanation.
	tvUI.cliCtx.nuxWorkspaceOpIntroIfNeeded(nuxKeyWorkspaceResetIntro)

	prompt := "Reset workspace sandbox only, or also change the origin repository for this thread?"
	defaultSandboxOnly := true
	sandboxOnly, err := tvUI.cliCtx.ui.SelectBool(
		prompt,
		types.UIOption{Key: "s", Label: "Reset sandbox only"},
		types.UIOption{Key: "o", Label: "Reset sandbox and change origin"},
		&defaultSandboxOnly,
	)
	if err != nil {
		return err
	}

	if sandboxOnly {
		suspendNCurses()
		err = tvUI.thread.Workspace().ResetSandbox(ctx)
		restoreNCurses()
		if err != nil {
			_ = tvUI.cliCtx.ui.Confirm(err.Error())
		}
		return err
	}

	if err := tvUI.thread.Workspace().Reset(); err != nil {
		_ = tvUI.cliCtx.ui.Confirm(err.Error())
		return err
	}
	_, _ = tvUI.setupWorkspace(ctx, true)

	return nil
}

type threadViewDiffMode int

const (
	threadViewDiffModeNone threadViewDiffMode = iota
	threadViewDiffModeUncommitted
	threadViewDiffModeSandboxOrigin
)

type threadViewDiffOptions struct {
	hasUncommitted       bool
	hasSandboxOriginDiff bool
}

func threadViewDiffOptionsFromStatus(st scm.RepoSyncStatus) threadViewDiffOptions {
	// Today, we treat "repo vs sandbox" as "sandbox local branch vs its upstream".
	// This approximates "origin vs sandbox" without needing a no-index directory
	// diff across the two checkouts.
	//
	// hasSandboxOriginDiff: any ahead/behind implies different commits.
	// hasUncommitted: includes staged/unstaged/untracked.
	return threadViewDiffOptions{
		hasUncommitted:       st.HasUncommittedChanges,
		hasSandboxOriginDiff: st.Ahead != 0 || st.Behind != 0,
	}
}

func (tvUI *threadViewUI) workspaceDiff(ctx context.Context) (needRedraw bool) {
	ws := tvUI.thread.Workspace()
	if ws.Sandbox() == "" {
		return false
	}

	st, err := tvUI.cliCtx.scmClient.RepoSyncStatus(ctx, ws.Sandbox())
	if err != nil {
		_ = tvUI.cliCtx.ui.Confirm(err.Error())
		return true
	}

	opts := threadViewDiffOptionsFromStatus(st)
	if !opts.hasUncommitted && !opts.hasSandboxOriginDiff {
		_ = tvUI.cliCtx.ui.Confirm("No differences found:\n\n- Sandbox has no uncommitted changes\n- Sandbox has no committed differences vs its upstream (origin)")
		return true
	}

	mode := threadViewDiffModeNone
	if opts.hasUncommitted && opts.hasSandboxOriginDiff {
		if ws.Origin() == "" {
			// Uncommitted diffs are still useful even when origin isn't configured.
			mode = threadViewDiffModeUncommitted
		} else {
			sel, selErr := tvUI.cliCtx.ui.SelectOption(
				"Diff what?",
				[]types.UIOption{
					{Key: "u", Label: "Sandbox: uncommitted changes vs most recent commit"},
					{Key: "r", Label: "Repo vs sandbox: committed differences between sandbox and its upstream (origin)"},
				},
			)
			if selErr != nil {
				_ = tvUI.cliCtx.ui.Confirm(selErr.Error())
				return true
			}
			switch sel.Key {
			case "u":
				mode = threadViewDiffModeUncommitted
			case "r":
				mode = threadViewDiffModeSandboxOrigin
			default:
				_ = tvUI.cliCtx.ui.Confirm("Invalid selection")
				return true
			}
		}
	} else if opts.hasUncommitted {
		defaultNo := false
		ok, selErr := tvUI.cliCtx.ui.SelectBool(
			"Sandbox has uncommitted changes. Open difftool vs most recent commit?",
			types.UIOption{Key: "y", Label: "Yes, diff uncommitted changes"},
			types.UIOption{Key: "n", Label: "No"},
			&defaultNo,
		)
		if selErr != nil {
			_ = tvUI.cliCtx.ui.Confirm(selErr.Error())
			return true
		}
		if !ok {
			return true
		}
		mode = threadViewDiffModeUncommitted
	} else if opts.hasSandboxOriginDiff {
		if ws.Origin() == "" {
			_ = tvUI.cliCtx.ui.Confirm("Workspace origin repo is not configured for this thread.\n\nCannot diff sandbox vs origin without an origin repo configured.")
			return true
		}

		defaultNo := false
		ok, selErr := tvUI.cliCtx.ui.SelectBool(
			"Sandbox differs from your repo (origin). Open difftool?",
			types.UIOption{Key: "y", Label: "Yes, diff repo vs sandbox"},
			types.UIOption{Key: "n", Label: "No"},
			&defaultNo,
		)
		if selErr != nil {
			_ = tvUI.cliCtx.ui.Confirm(selErr.Error())
			return true
		}
		if !ok {
			return true
		}
		mode = threadViewDiffModeSandboxOrigin
	}

	var spec scm.DiffSpec
	switch mode {
	case threadViewDiffModeUncommitted:
		spec = scm.DiffSpec{Scope: scm.DiffScopeUncommitted}
	case threadViewDiffModeSandboxOrigin:
		spec = scm.DiffSpec{Scope: scm.DiffScopeBranchUpstream}
	default:
		return false
	}

	// Suspend curses so the difftool can use the terminal.
	suspendNCurses()
	err = tvUI.cliCtx.scmClient.DiffTool(ctx, ws.Sandbox(), spec)
	restoreNCurses()
	if err != nil {
		_ = tvUI.cliCtx.ui.Confirm(err.Error())
	}

	return true
}
