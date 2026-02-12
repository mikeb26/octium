/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mikeb26/octium/internal/scm"
)

// DiffTool invokes `git difftool`.
//
// This is intended for interactive use (it wires stdio through), so it does not
// apply Client.Timeout when ctx has no deadline.

func (c *Client) DiffTool(ctx context.Context, dir string, spec scm.DiffSpec) error {
	toolName, toolKey, err := c.determineDiffToolName(ctx, dir)
	if err != nil {
		return err
	}
	if toolName == "" {
		return ErrDiffToolUnconfigured
	}

	gitArgs := []string{"difftool"}
	// If the user has configured a GUI-specific difftool, prefer it.
	if toolKey == "diff.guitool" || toolKey == "merge.guitool" {
		gitArgs = append(gitArgs, "--gui")
	}
	switch spec.Scope {
	case scm.DiffScopeUncommitted:
		// Show both staged and unstaged changes. git-difftool doesn't have a
		// single flag for this, so invoke it twice.
		//
		// Note: git-difftool does not include untracked files. We treat untracked
		// files as part of "uncommitted", so we additionally invoke difftool in
		// --no-index mode against os.DevNull for each untracked file.
		if err := c.runGitDiffTool(ctx, dir, append(gitArgs, "--cached", "--no-prompt")...); err != nil {
			return err
		}
		if err := c.runGitDiffTool(ctx, dir, append(gitArgs, "--no-prompt")...); err != nil {
			return err
		}

		untracked, err := c.untrackedFiles(ctx, dir)
		if err != nil {
			return err
		}
		for _, f := range untracked.Filename {
			// Compare to os.DevNull so additions show up as diffs.
			if err := c.runGitDiffTool(ctx, dir,
				append(gitArgs, "--no-index", "--no-prompt", "--", os.DevNull, f)...,
			); err != nil {
				return err
			}
		}
	case scm.DiffScopeBranchUpstream:
		upstream, branch, err := c.resolveBranchUpstreamDiffTargets(ctx, dir, spec.Branch)
		if err != nil {
			return err
		}
		return c.runGitDiffTool(ctx, dir, append(gitArgs, "--no-prompt", upstream, branch)...)
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedDiffScope, spec.Scope)
	}
	return nil
}

func (c *Client) resolveBranchUpstreamDiffTargets(ctx context.Context, dir string, branch string) (upstream string, resolvedBranch string, err error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		out, _, err := c.runWithTimeout(ctx, nil, buildGitArgs(dir, "symbolic-ref", "--quiet", "--short", "HEAD")...)
		if err != nil {
			return "", "", ErrFailedToDetermineBranch
		}
		branch = strings.TrimSpace(out)
		if branch == "" {
			return "", "", ErrFailedToDetermineBranch
		}
	}

	up, err := c.upstreamForBranch(ctx, dir, branch)
	if err != nil {
		return "", "", err
	}
	return up, branch, nil
}

func (c *Client) upstreamForBranch(ctx context.Context, dir string, branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", ErrBranchRequired
	}

	remoteKey := fmt.Sprintf("branch.%s.remote", branch)
	remote, ok, err := c.gitConfigGet(ctx, dir, remoteKey)
	if err != nil {
		return "", err
	}
	remote = strings.TrimSpace(remote)
	if !ok || remote == "" {
		return "", ErrNoUpstreamConfigured
	}

	mergeKey := fmt.Sprintf("branch.%s.merge", branch)
	merge, ok, err := c.gitConfigGet(ctx, dir, mergeKey)
	if err != nil {
		return "", err
	}
	merge = strings.TrimSpace(merge)
	if !ok || merge == "" {
		return "", ErrNoUpstreamConfigured
	}

	mergeShort := shortenMergeRef(merge)
	if mergeShort == "" {
		return "", ErrNoUpstreamConfigured
	}
	if remote == "." {
		return mergeShort, nil
	}
	return remote + "/" + mergeShort, nil
}

func shortenMergeRef(merge string) string {
	merge = strings.TrimSpace(merge)
	if merge == "" {
		return ""
	}
	const headsPrefix = "refs/heads/"
	if strings.HasPrefix(merge, headsPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(merge, headsPrefix))
	}
	return merge
}

func (c *Client) runGitDiffTool(ctx context.Context, dir string, gitArgs ...string) error {
	_, _, err := c.run(ctx, &RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}, buildGitArgs(dir, gitArgs...)...)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	return nil
}

func (c *Client) determineDiffToolName(ctx context.Context, dir string) (toolName string, toolKey string, err error) {
	// The selection order here mirrors git-difftool's documented behavior when
	// invoked with --gui.
	keys := []string{
		"diff.guitool",
		"merge.guitool",
		"diff.tool",
		"merge.tool",
	}

	for _, key := range keys {
		v, ok, err := c.gitConfigGet(ctx, dir, key)
		if err != nil {
			return "", "", err
		}
		v = strings.TrimSpace(v)
		if ok && v != "" {
			return v, key, nil
		}
	}

	return "", "", nil
}

func (c *Client) gitConfigGet(ctx context.Context, dir string, key string) (string, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	out, _, err := c.run(ctx, nil, buildGitArgs(dir, "config", "--get", key)...)
	if err == nil {
		v := strings.TrimSpace(out)
		if v == "" {
			return "", false, nil
		}
		return v, true, nil
	}

	// Exit code 1 indicates the key was not found.
	if isGitExitCode(err, 1) {
		return "", false, nil
	}
	return "", false, fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
}
