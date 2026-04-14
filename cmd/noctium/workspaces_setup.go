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
	"io/fs"
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
	ignoreUnset bool) (bool, error) {

	if tvUI.isArchived {
		return false, fmt.Errorf("thread is archived")
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
		return false, err
	}

	// if we have a non-empty origin, that means at this point we've validated
	// it still exists, the sandbox still exists, and the sandbox points back
	// to the origin. so the workspace is setup
	if ws.Origin() != "" {
		return false, nil
	}

	if ws.IsUnset() && !ignoreUnset {
		return false, ErrWorkspaceNotConfigured
	}

	// First try pwd; if it's a repo, confirm with the user.
	//
	// Note: pwd may be a subdirectory of the repo; when linking a workspace we
	// always want the repo root.
	pwd, err := os.Getwd()
	if pwd == "" {
		if err == nil {
			err = fmt.Errorf("cannot determine working directory")
		}
		return false, err
	}
	if _, err := tvUI.cliCtx.scmClient.RepoStatusString(ctx, pwd); err == nil {
		pwdAbs, absErr := filepath.Abs(pwd)
		if absErr == nil {
			pwd = filepath.Clean(pwdAbs)
		} else {
			pwd = filepath.Clean(pwd)
		}

		repoRoot := pwd
		if root, ok, rootErr := findNearestGitRepoRoot(pwd); rootErr == nil && ok {
			// Extra safety: ensure the computed root is still treated as a git repo by
			// the configured SCM client.
			if _, stErr := tvUI.cliCtx.scmClient.RepoStatusString(ctx, root); stErr == nil {
				repoRoot = root
			}
		}

		var prompt string
		if repoRoot == pwd {
			prompt = fmt.Sprintf("A git repository was detected in your current working directory:\n%v\n\nLink this thread's workspace to this repository?", repoRoot)
		} else {
			prompt = fmt.Sprintf("A git repository was detected in a parent directory of your current working directory:\n(current dir: %v)\n(repo root:   %v)\n\nLink this thread's workspace to this repository?", pwd, repoRoot)
		}
		usePwd, err := tvUI.cliCtx.ui.SelectOption(
			prompt,
			[]types.UIOption{{Key: "y", Label: "Yes, use it"},
				{Key: "n", Label: "No, choose another"},
				{Key: "x", Label: "No, I will configure the workspace later if needed"}})
		if err != nil {
			return false, err
		}
		if usePwd.Key == "x" {
			ws.Destroy()
			return false, ErrWorkspaceSetupCancelled
		}
		if usePwd.Key == "y" {
			suspendNCurses()
			err = ws.AddOriginAndSandbox(ctx, repoRoot)
			restoreNCurses()
			return true, err
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
		return false, err
	}
	if repoDir == "" {
		return false, ErrWorkspaceSetupCancelled
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
		return false, err
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
			return false, err
		}
		if !create {
			return false, ErrWorkspaceSetupCancelled
		}
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			return false, fmt.Errorf("failed to create dir %v: %w", repoDir, err)
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
				return false, err
			}
			if !create {
				return false, ErrWorkspaceSetupCancelled
			}
		}
		if err := tvUI.initNewGitRepo(ctx, repoDir); err != nil {
			return false, err
		}
	} else {
		// If the user typed a subdirectory of a git repo, normalize to repo root.
		if root, ok, rootErr := findNearestGitRepoRoot(repoDir); rootErr == nil && ok {
			repoDir = root
		}
	}

	suspendNCurses()
	err = ws.AddOriginAndSandbox(ctx, repoDir)
	restoreNCurses()

	return true, err
}

// findNearestGitRepoRoot walks up from start and returns the nearest ancestor
// directory that contains a ".git" entry (file or directory).
//
// This is used to normalize a working directory (which may be a subdirectory
// within a repo) into the actual repository root directory.
func findNearestGitRepoRoot(start string) (string, bool, error) {
	if strings.TrimSpace(start) == "" {
		return "", false, nil
	}

	startAbs, err := filepath.Abs(filepath.Clean(start))
	if err != nil {
		return "", false, err
	}

	dir := startAbs
	for {
		gitPath := filepath.Join(dir, ".git")
		_, err := os.Stat(gitPath)
		if err == nil {
			return dir, true, nil
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", false, err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return startAbs, false, nil
		}
		dir = parent
	}
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
