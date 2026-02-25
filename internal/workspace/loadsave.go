/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/mikeb26/octium/internal"
	"github.com/negrel/assert"
)

const (
	WorkspaceFileName = "ws.json"
)

type persistedWorkspace struct {
	RepoExplicitlyUnset bool   `json:"repo_unset"`
	ScratchDir          string `json:"scratch_dir"`
	OriginRepo          string `json:"origin_repo"`
	SboxRepo            string `json:"sbox_repo"`
	Id                  string
}

// Save persists the workspace to scratchDir/WorkspaceFileName.
func (ws *Workspace) Save() error {
	scratchDir := ws.persisted.ScratchDir
	assert.NotEmpty(scratchDir)

	if err := os.MkdirAll(scratchDir, 0o700); err != nil {
		return fmt.Errorf("%w %v: %w", ErrScratchDirCreate, scratchDir, err)
	}

	content, err := json.Marshal(&ws.persisted)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWorkspaceMarshal, err)
	}

	tmpPath := filepath.Join(scratchDir, WorkspaceFileName+".tmp")
	finalPath := filepath.Join(scratchDir, WorkspaceFileName)

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w %v: %w", ErrWorkspaceOpenFile, tmpPath, err)
	}
	defer f.Close()

	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("%w %v: %w", ErrWorkspaceWriteFile, tmpPath, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("%w %v: %w", ErrWorkspaceSyncFile, tmpPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%w %v: %w", ErrWorkspaceCloseFile, tmpPath, err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("%w %v: %w", ErrWorkspacePersist, finalPath, err)
	}

	return nil
}

// Load restores the workspace
//
// At load time,
//   - if OriginRepo is non-empty, validates it exists, is a directory, and
//     is an scm repository
//   - if SboxRepo is non-empty, validates it exists, is a directory,
//     is an scm repository, and has an origin remote pointing to originRepo
func (ws *Workspace) Load(ctx context.Context) error {
	scratchDir := ws.persisted.ScratchDir
	if scratchDir == "" {
		return fmt.Errorf("%w", ErrScratchDirNotSet)
	}

	path := filepath.Join(scratchDir, WorkspaceFileName)
	text, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ws.Save()
		}
		return fmt.Errorf("%w %v: %w", ErrWorkspaceReadFile, path, err)
	}

	var loaded persistedWorkspace
	if err := json.Unmarshal(text, &loaded); err != nil {
		return fmt.Errorf("%w %v: %w", ErrWorkspaceParseFile, path, err)
	}

	if loaded.ScratchDir == "" {
		loaded.ScratchDir = scratchDir
	}
	match, err := normalizedDirEqual(loaded.ScratchDir, scratchDir)
	if err != nil {
		return err
	}
	if !match {
		return fmt.Errorf("%w: file has %v, expected %v", ErrWorkspaceScratchMismatch, loaded.ScratchDir, scratchDir)
	}

	if loaded.OriginRepo != "" {
		if err := ws.validateScmRepoDir(ctx, "Origin", loaded.OriginRepo); err != nil {
			return fmt.Errorf("%w: %w", ErrOriginRepoInvalid, err)
		}
	}

	loaded.ScratchDir = scratchDir
	ws.persisted = loaded

	if loaded.SboxRepo != "" {
		if loaded.OriginRepo == "" {
			return fmt.Errorf("%w", ErrWorkspaceSandboxSetOriginEmpty)
		}
		if err := ws.validateScmRepoDir(ctx, "Sandbox", loaded.SboxRepo); err != nil {
			return fmt.Errorf("%w: %w", ErrSandboxRepoInvalid, err)
		}
		if err := ws.validateSandboxOriginRemote(ctx, loaded.SboxRepo, loaded.OriginRepo); err != nil {
			return fmt.Errorf("%w: %w", ErrSandboxRepoInvalid, err)
		}
	}

	return nil
}

func fixupSharedDirPerms(dir string) error {
	var firstErr error
	if err := fixupSharedDirPermsTry(dir, true); err != nil {
		firstErr = err
		// Some environments disallow setgid, even for user-owned dirs.
		// Retry without setgid. Keep the original error if retry also fails.
		if retryErr := fixupSharedDirPermsTry(dir, false); retryErr == nil {
			return nil
		}
	}

	return firstErr
}

func fixupSharedDirPermsTry(dir string, includeSetgid bool) error {
	var firstErr error
	dirMode := os.FileMode(0o770)
	if includeSetgid {
		dirMode |= os.ModeSetgid
	}
	filMode := os.FileMode(0o660)

	gid, gidErr := sandboxSharedGID()
	if gidErr != nil {
		// Best effort: if we can't resolve the group (e.g. in certain tests),
		// keep going with chmod-only behavior.
		gid = -1
	}

	if gid >= 0 {
		if err := os.Chown(dir, -1, gid); err != nil {
			firstErr = fmt.Errorf("failed to chgrp dir %v: %w", dir, err)
		}
	}

	if err := os.Chmod(dir, dirMode); err != nil {
		if firstErr == nil {
			firstErr = fmt.Errorf("failed to chmod dir %v: %w", dir, err)
		}
	}
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry,
		walkErr error) error {

		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		if d.Type().IsRegular() {
			if gid >= 0 {
				if err := os.Chown(path, -1, gid); err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("chgrp %q: %w", path, err)
					}
				}
			}
			if err := os.Chmod(path, filMode); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("chmod %q: %w", path, err)
				}
			}
		} else if d.IsDir() {
			if gid >= 0 {
				if err := os.Chown(path, -1, gid); err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("chgrp %q: %w", path, err)
					}
				}
			}
			if err := os.Chmod(path, dirMode); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("chmod %q: %w", path, err)
				}
			}
		}

		return nil
	}); err != nil {
		if firstErr == nil {
			firstErr = fmt.Errorf("failed to chmod subdirectories for %v: %w",
				dir, err)
		}
	}

	return firstErr
}

func sandboxSharedGID() (int, error) {
	endUser := internal.CliEndUsername()
	groupName := internal.CliSandboxGroupname(endUser)

	g, err := user.LookupGroup(groupName)
	if err != nil {
		return -1, err
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return -1, err
	}

	return gid, nil
}
