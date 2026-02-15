/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mikeb26/octium/internal/types"
)

func getWorkspacePwdFromCtx(ctx context.Context) (string, error) {
	ws, ok := types.GetWorkspacePwd(ctx)
	if !ok || strings.TrimSpace(ws) == "" {
		return "", ErrWorkspacePwdNotSet
	}
	ws = filepath.Clean(ws)
	if !filepath.IsAbs(ws) {
		// We intentionally avoid using the Go process's working directory.
		return "", fmt.Errorf("%w: %q", ErrWorkspacePwdNotAbs, ws)
	}
	return ws, nil
}

func resolvePathWithinWorkspace(ctx context.Context, p string) (string, error) {
	ws, err := getWorkspacePwdFromCtx(ctx)
	if err != nil {
		return "", err
	}

	// Note: this function performs a lexical containment check only.
	// File tools must still open paths using fsparanoid.Open (openat2 with
	// RESOLVE_BENEATH/RESOLVE_NO_SYMLINKS) to defend against symlink-based escapes
	// and TOCTOU races in hostile workspaces.
	abs := p
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(ws, p))
	}

	if !isWithinWorkspace(ws, abs) {
		return "", fmt.Errorf("%w: %q is not within workspace %q", ErrPathOutsideWorkspace, abs, ws)
	}

	return abs, nil
}

func isWithinWorkspace(workspace, absPath string) bool {
	ws := filepath.Clean(workspace)
	p := filepath.Clean(absPath)

	rel, err := filepath.Rel(ws, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
