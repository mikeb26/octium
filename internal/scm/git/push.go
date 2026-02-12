/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/mikeb26/octium/internal/scm"
)

// Push pushes the current branch to a remote.
//
// If branch is empty, it pushes to the current branch's configured upstream
// (when available). Similarly when both remoteName and branch are empty.
//
// Implementations should avoid interactive prompting.
func (c *Client) Push(ctx context.Context, dir string, remoteName string, branch string) error {
	meta, flags, err := c.readPorcelainMetaFlags(ctx, dir)
	if err != nil {
		return err
	}
	if flags.staged || flags.unstaged || flags.untracked {
		return fmt.Errorf("local changes are present; refusing to push")
	}
	if mapOperation(c.inProgressOperation(ctx, dir)) != scm.RepoOperationNormal {
		return fmt.Errorf("repository has an in-progress operation; refusing to push")
	}

	pushRemote, pushBranch, err := c.resolvePushTarget(remoteName, branch, meta)
	if err != nil {
		return err
	}

	// Prefer an explicit refspec so behavior is stable even when push.default is
	// unusual. This mirrors the upstream-based semantics requested by the caller.
	refspec := "HEAD:refs/heads/" + pushBranch

	_, _, err = c.runWithTimeout(ctx, nil, buildGitArgs(dir, "push", pushRemote, refspec)...)
	if err != nil {
		if isGitExitCode(err, 1) {
			return scm.ErrPushRejected
		}
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	return nil
}

func (c *Client) resolvePushTarget(remoteName string, branch string, meta porcelainMeta) (remote string, pushBranch string, err error) {
	remoteName = strings.TrimSpace(remoteName)
	branch = strings.TrimSpace(branch)

	if remoteName == "" && branch == "" {
		remote, pushBranch = splitUpstream(meta.upstream)
		if remote == "" || pushBranch == "" {
			return "", "", ErrNoUpstreamConfigured
		}
		return remote, pushBranch, nil
	}

	if remoteName == "" || branch == "" {
		// Mirror Merge's "upstream-first" behavior: partial specification isn't
		// supported in the public interface.
		return "", "", ErrRemoteAndBranchRequired
	}

	return remoteName, branch, nil
}
