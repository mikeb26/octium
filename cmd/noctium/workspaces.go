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
	"github.com/mikeb26/octium/internal/threads"
	"github.com/mikeb26/octium/internal/types"
)

// launchWorkspaceModalFromThreadView opens a selection modal for workspace
// operations.
func (tvUI *threadViewUI) launchWorkspaceModalFromThreadView(ctx context.Context) error {
	if tvUI.ws.Origin() == "" {
		err := tvUI.setupWorkspace(ctx, true)
		if err != nil {
			if errors.Is(err, ErrWorkspaceSetupCancelled) || errors.Is(err, ErrWorkspaceNotConfigured) {
				return nil
			}
			_ = tvUI.cliCtx.ui.Confirm(fmt.Sprintf("Workspace setup failed:\n%v", err))
			return err
		}
	}

	choices := []types.UIOption{
		{Key: "d", Label: "Diff:d Show differences in the workspace sandbox"},
		{Key: "c", Label: "Commit:c Commit any uncommitted changes that may be present in the workspace sandbox"},
		{Key: "s", Label: "Sync:s Synchronize the workspace's sandbox from your repo"},
		{Key: "p", Label: "Push:p Push committed changes in the workspace's sandbox into a branch in your repo"},
		{Key: "m", Label: "Merge:m Merge committed changes in the workspace's sandbox into your repo's current branch"},
		{Key: "r", Label: "Reset:r Reset the workspace sandbox and/or change which of your repositories this thread works with"},
	}

	sel, err := tvUI.cliCtx.ui.SelectOption(tvUI.ws.Detail(), choices)
	if err != nil {
		// cancel
		return err
	}

	switch sel.Key {
	case "d":
		_ = tvUI.launchDiffToolFromThreadView(ctx)
	case "c":
		_ = tvUI.launchCommitFromThreadView(ctx)
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

func workspaceSync(ctx context.Context, tvUI *threadViewUI) error {
	suspendNCurses()
	err := tvUI.ws.SyncSandbox(ctx, true)
	restoreNCurses()
	if err != nil {
		_ = tvUI.cliCtx.ui.Confirm(err.Error())
	}

	return err
}

func workspacePush(ctx context.Context, tvUI *threadViewUI) error {
	dstBranch := branchNameFromThread(tvUI.thread)
	suspendNCurses()
	err := tvUI.ws.PushSandbox(ctx, dstBranch)
	restoreNCurses()
	if err != nil {
		_ = tvUI.cliCtx.ui.Confirm(fmt.Sprintf("%v\nDestination branch: %v", err.Error(), dstBranch))
		return err
	}

	_ = tvUI.cliCtx.ui.Confirm(fmt.Sprintf("Successfully pushed to branch %v", dstBranch))
	return nil
}

func workspaceMerge(ctx context.Context, tvUI *threadViewUI) error {
	suspendNCurses()
	err := tvUI.ws.MergeSandbox(ctx)
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
		err = tvUI.ws.ResetSandbox(ctx)
		restoreNCurses()
		if err != nil {
			_ = tvUI.cliCtx.ui.Confirm(err.Error())
		}
		return err
	}

	if err := tvUI.ws.Reset(); err != nil {
		_ = tvUI.cliCtx.ui.Confirm(err.Error())
		return err
	}
	_ = tvUI.setupWorkspace(ctx, true)

	return nil
}
