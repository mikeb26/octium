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
	"path/filepath"

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

	// Ensure the sandbox parent directory exists even before a sandbox is set.
	//
	// This is intentionally done here (rather than during sandbox creation)
	// so callers can rely on Workspace.GetPwd() returning an existing directory
	// for the sandbox parent when no sandbox is present.
	baseSandboxDir, err := ws.getSandboxDir("")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(baseSandboxDir, 0o770|os.ModeSetgid); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("%w %v: %w", ErrSandboxParentDirPermission, baseSandboxDir, err)
		}
		return fmt.Errorf("%w %v: %w", ErrSandboxParentDirCreate, baseSandboxDir, err)
	}
	if err := fixupSharedDirPerms(baseSandboxDir); err != nil {
		return fmt.Errorf("%w %v: %w", ErrSandboxParentDirChmod, baseSandboxDir, err)
	}

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

	loaded.ScratchDir = scratchDir
	ws.persisted = loaded

	return nil
}

func fixupSharedDirPerms(dir string) error {
	dirMode := os.FileMode(0o775) | os.ModeSetgid
	filMode := os.FileMode(0o664)
	if err := os.Chmod(dir, dirMode); err != nil {
		return fmt.Errorf("failed to chmod dir %v: %w", dir, err)
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
			if err := os.Chmod(path, filMode); err != nil {
				return fmt.Errorf("chmod %q: %w", path, err)
			}
		} else if d.IsDir() {
			if err := os.Chmod(path, dirMode); err != nil {
				return fmt.Errorf("chmod %q: %w", path, err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to chmod subdirectories for %v: %w", dir, err)
	}

	return nil
}
