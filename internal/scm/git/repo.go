/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// InitRepo initializes a new git repository at dir.
//
// This method is intended for interactive/UI-triggered use. It does not apply
// Client.Timeout when ctx has no deadline.
func (c *Client) InitRepo(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %v: %w", dir, err)
	}

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "init")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	return nil
}

// CloneRepo clones repoURL into dir.
//
// This method is intended for interactive/UI-triggered use. It does not apply
// Client.Timeout when ctx has no deadline.
func (c *Client) CloneRepo(ctx context.Context, repoURL string, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %v: %w", dir, err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", repoURL, dir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	return nil
}
