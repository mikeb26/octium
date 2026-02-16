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

// Commit stages and commits changes.
//
// Behavior:
//   - First compiles the list of untracked files present in the repo.
//   - If any untracked file is not mentioned in opts.IncludeUntracked, returns
//     scm.ErrUntrackedFiles (and includes the full list in the return).
//   - Always stages tracked changes (equivalent to `git add -u`).
//   - If all untracked files are mentioned in opts.IncludeUntracked, stages
//     only those with map values of true.
//
// This method is intended for interactive/UI-triggered use. It does not apply
// Client.Timeout when ctx has no deadline.
func (c *Client) Commit(ctx context.Context, dir string, opts scm.CommitOptions) (*scm.UntrackedFiles, error) {
	// Interactive commits (`git commit` without -m) are not compatible with the
	// sandbox execution mode.
	//
	// The sandbox wrapper executes the command inside a transient systemd unit
	// without a pty/tty, and git editors typically require a usable terminal.
	if opts.RunAs == scm.ExecAsSandboxUser && opts.Message == "" {
		return nil, scm.ErrInteractiveCommitUnsupported
	}

	untracked, err := c.untrackedFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	if len(untracked.Filename) > 0 {
		missing := false
		for _, f := range untracked.Filename {
			_, ok := opts.IncludeUntracked[f]
			if !ok {
				missing = true
				break
			}
		}
		if missing {
			return untracked, scm.ErrUntrackedFiles
		}
	}

	var runOpts RunOptions
	runOpts.RunAs = opts.RunAs
	// Stage tracked changes.
	if _, _, err := c.run(ctx, &runOpts, buildGitArgs(dir, "add", "-u")...); err != nil {
		return nil, err
	}

	// Optionally stage selected untracked files.
	if len(untracked.Filename) > 0 {
		for _, f := range untracked.Filename {
			if opts.IncludeUntracked[f] {
				// Stage this specific untracked file (do not use -u, which only updates
				// tracked files).
				if _, _, err := c.run(ctx, &runOpts, buildGitArgs(dir, "add", "--", f)...); err != nil {
					return nil, err
				}
			}
		}
	}

	// If nothing is staged after our add operations, there's nothing for an
	// interactive `git commit` to do (it would just exit with "nothing to
	// commit"). Return a sentinel error so callers can handle this case
	// explicitly.
	staged, _, err := c.run(ctx, nil, buildGitArgs(dir, "diff", "--cached", "--name-only")...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(staged) == "" {
		return nil, scm.ErrNothingToCommit
	}

	if opts.Message == "" {
		// Run an interactive commit so git can invoke the user's configured editor.
		//
		// We wire stdio through so that terminal-based editors work, and so that git
		// can prompt/confirm as needed.
		_, _, err = c.run(ctx, &RunOptions{RunAs: opts.RunAs, Stdin: os.Stdin, Stdout: os.Stdout,
			Stderr: os.Stderr}, buildGitArgs(dir, "commit")...)
	} else {
		_, _, err = c.run(ctx, &runOpts, buildGitArgs(dir, "commit", "--no-edit",
			"-m", opts.Message)...)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	return nil, nil
}

func (c *Client) untrackedFiles(ctx context.Context, dir string) (*scm.UntrackedFiles, error) {
	ret := &scm.UntrackedFiles{
		Filename: make([]string, 0),
	}

	out, _, err := c.run(ctx, nil, buildGitArgs(dir, "ls-files", "--others", "--exclude-standard")...)
	if err != nil {
		return ret, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return ret, nil
	}
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		f := strings.TrimSpace(line)
		if f == "" {
			continue
		}
		ret.Filename = append(ret.Filename, f)
	}
	return ret, nil
}
