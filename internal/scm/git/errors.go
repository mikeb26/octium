/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"errors"
)

var (
	ErrFailedToExecuteGit      = errors.New("failed to execute git")
	ErrFailedToDetermineBranch = errors.New("failed to determine current git branch")
	ErrDiffToolUnconfigured    = errors.New("no difftool configured; please run git config --global diff.tool to configure")
	ErrUnsupportedDiffScope    = errors.New("unsupported diff scope")
	ErrBranchRequired          = errors.New("branch is required")
	ErrRemoteNameRequired      = errors.New("remote name is required")
	ErrRemoteURLRequired       = errors.New("remote URL is required")
	ErrNoUpstreamConfigured    = errors.New("no upstream configured")
	ErrNotGitRepo              = errors.New("not a git repo")
	ErrFailedToInitRepo        = errors.New("failed to initialize git repo")
)
