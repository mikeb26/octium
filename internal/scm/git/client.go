/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Client is a small abstraction over calling the git executable.
//
// gptcli assumes git is installed and available, and uses this client
// as the backend for git-related functionality.
//
// Keeping all git process invocation behind this type makes it easier
// to:
//   - add timeouts/cancellation consistently (CommandContext)
//   - inject a fake client in tests (later, if needed)
//   - centralize logging/telemetry later
//   - evolve to higher level git helpers without exec sprawl
//
// NOTE: this is intentionally minimal and should grow as the repository
// adds more git features (commit/merge/etc).
type Client struct {
	// Timeout is applied when ctx has no deadline.
	Timeout time.Duration
}

// RunOptions customizes how git is executed.
//
// By default, stdout/stderr are captured and returned.
// If Stdout/Stderr are set, output will be written there instead
type RunOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func NewClient() *Client {
	return &Client{Timeout: 750 * time.Millisecond}
}

func (c *Client) runWithTimeout(ctx context.Context, opts *RunOptions, args ...string) (string, string, error) {
	if _, ok := ctx.Deadline(); !ok && c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	return c.run(ctx, opts, args...)
}

func (c *Client) run(ctx context.Context, opts *RunOptions, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)

	var out bytes.Buffer
	var errBuf bytes.Buffer

	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if opts != nil {
		cmd.Stderr = opts.Stderr
		cmd.Stdout = opts.Stdout
		cmd.Stdin = opts.Stdin
	}

	outStr := ""
	errStr := ""
	err := cmd.Run()
	if opts == nil {
		outStr = out.String()
		errStr = errBuf.String()
	}
	if err != nil {
		return outStr, errStr, fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}

	return outStr, errStr, nil
}

func isGitExitCode(err error, code int) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	return ee.ExitCode() == code
}
