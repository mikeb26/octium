/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"context"
	"fmt"
)

// ShareRepo configures a repository so it can be shared across users.
//
// It is equivalent to:
//
//	git config core.sharedRepository <group>
func (c *Client) ShareRepo(ctx context.Context, dir string) error {
	_, _, err := c.runWithTimeout(ctx, nil, buildGitArgs(dir, "config", "core.sharedRepository", "true")...)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	_, _, err = c.runWithTimeout(ctx, nil, buildGitArgs(dir, "config", "core.fileMode", "false")...)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}

	return nil
}
