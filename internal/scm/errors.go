/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package scm

import (
	"errors"
)

var (
	ErrUntrackedFiles  = errors.New("untracked files are present")
	ErrNothingToCommit = errors.New("there is nothing to commit")
	// ErrInteractiveCommitUnsupported indicates that the SCM backend cannot
	// perform an interactive commit (e.g. invoking $EDITOR) in the requested
	// execution mode.
	//
	// Today this is used when attempting to run `git commit` as the sandbox/AI
	// user, which executes inside a transient systemd unit without a TTY.
	ErrInteractiveCommitUnsupported = errors.New("interactive commit is not supported")
	ErrMergeConflicts  = errors.New("merge would result in conflicts")
	ErrPushRejected    = errors.New("push was rejected")
	ErrBranchRequired  = errors.New("branch is required")
)
