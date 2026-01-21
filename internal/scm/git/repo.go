/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"context"
	"fmt"
	"os"
)

// InitRepo initializes a new git repository at dir.
//
// This method is intended for interactive/UI-triggered use. It does not apply
// Client.Timeout when ctx has no deadline.
func (c *Client) InitRepo(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %v: %w", dir, err)
	}

	_, _, err := c.run(ctx, &RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}, "-C", dir, "init")
	if err != nil {
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

	_, _, err := c.run(ctx, &RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}, "clone", repoURL, dir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	return nil
}
