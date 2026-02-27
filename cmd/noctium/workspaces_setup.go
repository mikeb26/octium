/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/scm"
	"github.com/mikeb26/octium/internal/types"
	"github.com/mikeb26/octium/internal/workspace"
)

func friendlyWorkspaceSetupErr(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, workspace.ErrSandboxParentDirPermission) {
		endUser := internal.CliEndUsername()
		group := internal.CliSandboxGroupname(endUser)
		return fmt.Sprintf(
			"Workspace setup failed: %v\n\nYour current login session does not have the updated supplementary groups (it was started before you were added to '%s'). Linux determines a process's group list when you start a login session, and running processes don't automatically re-check /etc/group.\n\nFix: log out completely and log back in (or run 'newgrp %s' in your shell) so new processes include the '%s' group, then retry.",
			err,
			group,
			group,
			group,
		)
	}

	return err.Error()
}

//go:embed amd.template
var agentsMdTmplText string

// if we dont have a git repository associated with a thread, associate one
// validate it exists (is a directory and is a git repository)
// if it doesn't exist, create it
func (tvUI *threadViewUI) setupWorkspace(ctx context.Context,
	ignoreUnset bool) error {

	if tvUI.isArchived {
		return fmt.Errorf("thread is archived")
	}
	ws := tvUI.thread.Workspace()
	err := ws.Load(ctx)
	if errors.Is(err, workspace.ErrOriginRepoInvalid) {
		// the user likely deleted the original repository, so we have
		// lost context. just reset.
		// @todo should prompt user to confirm
		_ = ws.Reset()
	} else if errors.Is(err, workspace.ErrSandboxRepoInvalid) {
		// the user or llm likely directly altered the sandbox, so we
		// have lost context. reset the sandbox.
		// @todo should prompt user to confirm
		suspendNCurses()
		err = ws.ResetSandbox(ctx)
		restoreNCurses()
	}
	if err != nil {
		return err
	}

	// if we have a non-empty origin, that means at this point we've validated
	// it still exists, the sandbox still exists, and the sandbox points back
	// to the origin. so the workspace is setup
	if ws.Origin() != "" {
		return nil
	}

	if ws.IsUnset() && !ignoreUnset {
		return ErrWorkspaceNotConfigured
	}

	// first try pwd; if it's a repo confirm with the user
	pwd, err := os.Getwd()
	if pwd == "" {
		if err == nil {
			err = fmt.Errorf("cannot determine working directory")
		}
		return err
	}
	if _, err := tvUI.cliCtx.scmClient.RepoStatusString(ctx, pwd); err == nil {
		prompt := fmt.Sprintf("A git repository was detected in your current working directory:\n%v\n\nLink this thread's workspace to this repository?", pwd)
		usePwd, err := tvUI.cliCtx.ui.SelectOption(
			prompt,
			[]types.UIOption{{Key: "y", Label: "Yes, use it"},
				{Key: "n", Label: "No, choose another"},
				{Key: "x", Label: "No, I will configure the workspace later if needed"}})
		if err != nil {
			return err
		}
		if usePwd.Key == "x" {
			ws.Destroy()
			return ErrWorkspaceSetupCancelled
		}
		if usePwd.Key == "y" {
			if !filepath.IsAbs(pwd) {
				pwd2, err := filepath.Abs(pwd)
				if err != nil {
					pwd = pwd2
				}
			}
			pwd = filepath.Clean(pwd)
			suspendNCurses()
			err = ws.AddOriginAndSandbox(ctx, pwd)
			restoreNCurses()
			return err
		}
	}

	// ok here we know we still haven't setup an origin and the user doesn't
	// want pwd, so ask
	prompt := "Enter a repository directory to link this thread's workspace (ESC to cancel):"
	if pwd != "" {
		prompt = fmt.Sprintf("%v\n(current dir: %v)", prompt, pwd)
	}
	repoDir, err := tvUI.cliCtx.ui.Get(prompt)
	repoDir = strings.TrimSpace(repoDir)
	if err != nil {
		return err
	}
	if repoDir == "" {
		return ErrWorkspaceSetupCancelled
	}

	// Normalize to an absolute path for persistence.
	if !filepath.IsAbs(repoDir) {
		if pwd != "" {
			repoDir = filepath.Join(pwd, repoDir)
		}
		abs, absErr := filepath.Abs(repoDir)
		if absErr == nil {
			repoDir = abs
		}
	}
	repoDir = filepath.Clean(repoDir)
	repoDirExists, err := dirExists(repoDir)
	if err != nil {
		return err
	}
	create := false
	if !repoDirExists {
		prompt := fmt.Sprintf("Directory does not exist:\n%v\n\nCreate repo here?", repoDir)
		defaultNo := false
		create, err = tvUI.cliCtx.ui.SelectBool(
			prompt,
			types.UIOption{Key: "y", Label: "Yes, create"},
			types.UIOption{Key: "n", Label: "No, do not create"},
			&defaultNo,
		)
		if err != nil {
			return err
		}
		if !create {
			return ErrWorkspaceSetupCancelled
		}
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			return fmt.Errorf("failed to create dir %v: %w", repoDir, err)
		}
	}

	if _, err := tvUI.cliCtx.scmClient.RepoStatusString(ctx, repoDir); err != nil {
		if !create {
			prompt := fmt.Sprintf("No git repository detected in:\n%v\n\nInitialize a new git repository here?", repoDir)
			defaultNo := false
			create, err = tvUI.cliCtx.ui.SelectBool(
				prompt,
				types.UIOption{Key: "y", Label: "Yes, initialize"},
				types.UIOption{Key: "n", Label: "No"},
				&defaultNo,
			)
			if err != nil {
				return err
			}
			if !create {
				return ErrWorkspaceSetupCancelled
			}
		}
		if err := tvUI.initNewGitRepo(ctx, repoDir); err != nil {
			return err
		}
	}

	suspendNCurses()
	err = ws.AddOriginAndSandbox(ctx, repoDir)
	restoreNCurses()

	return err
}

func (tvUI *threadViewUI) initNewGitRepo(ctx context.Context, repoDir string) error {
	if err := tvUI.cliCtx.scmClient.InitRepo(ctx, repoDir); err != nil {
		return err
	}

	agentsPath := filepath.Join(repoDir, AgentsMD)
	if err := os.WriteFile(agentsPath, []byte(agentsMdTmplText), 0o644); err != nil {
		return fmt.Errorf("failed to write %v: %w", agentsPath, err)
	}

	commitMsg := fmt.Sprintf("Add %v", AgentsMD)
	include := map[string]bool{AgentsMD: true}
	_, err := tvUI.cliCtx.scmClient.Commit(ctx, repoDir, scm.CommitOptions{Message: commitMsg, IncludeUntracked: include})
	return err
}

func dirExists(path string) (bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !st.IsDir() {
		return false, fmt.Errorf("path exists but is not a directory: %v", path)
	}
	return true, nil
}
