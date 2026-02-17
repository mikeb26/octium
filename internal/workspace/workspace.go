/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package workspace

import (
	"context"
	"fmt"
	"os"

	"github.com/mikeb26/octium/internal/scm"
	"github.com/negrel/assert"
)

type Workspace struct {
	persisted            persistedWorkspace
	scmClient            scm.Client
	sandboxParentCreated bool
}

func New(scratchDirIn string, idIn string, scmClientIn scm.Client) *Workspace {
	return &Workspace{
		persisted: persistedWorkspace{
			RepoExplicitlyUnset: false,
			ScratchDir:          scratchDirIn,
			Id:                  idIn,
		},
		scmClient: scmClientIn,
	}
}

func (ws *Workspace) Detail() string {
	return fmt.Sprintf("Workspace:\n\tScratch:%v\n\tOrigin:%v\n\tSandbox:%v\n",
		ws.persisted.ScratchDir, ws.persisted.OriginRepo, ws.persisted.SboxRepo)
}

func (ws *Workspace) String(ctx context.Context) string {
	if ws.persisted.OriginRepo == "" {
		return ""
	}
	originRepoStatus, err := ws.scmClient.RepoStatusString(ctx,
		ws.persisted.OriginRepo)
	if err != nil {
		originRepoStatus = "<unknown>"
	}
	sboxRepoStatus, err := ws.scmClient.RepoStatusString(ctx,
		ws.persisted.SboxRepo)
	if err != nil {
		sboxRepoStatus = "<unknown>"
	}

	return fmt.Sprintf("Origin:[%s] Sandbox:[%s]", originRepoStatus,
		sboxRepoStatus)
}

func (ws *Workspace) Destroy() error {
	ws.persisted.RepoExplicitlyUnset = true
	return ws.Reset()
}

func (ws *Workspace) IsUnset() bool {
	return ws.persisted.RepoExplicitlyUnset
}

func (ws *Workspace) Reset() error {
	if ws.persisted.SboxRepo != "" {
		// best effort
		_ = os.RemoveAll(ws.persisted.SboxRepo)
	}
	// best effort
	if baseSandboxDir, err := ws.getSandboxDir(""); err == nil {
		_ = os.RemoveAll(baseSandboxDir)
		ws.sandboxParentCreated = false
	}
	ws.persisted.OriginRepo = ""
	ws.persisted.SboxRepo = ""

	return ws.Save()
}

func (ws *Workspace) Sandbox() string {
	return ws.persisted.SboxRepo
}

func (ws *Workspace) Origin() string {
	return ws.persisted.OriginRepo
}

func (ws *Workspace) Sratch() string {
	return ws.persisted.ScratchDir
}

func (ws *Workspace) ResetSandbox(ctx context.Context) error {
	if ws.persisted.OriginRepo == "" {
		return fmt.Errorf("%w (reset)", ErrWorkspaceNoOriginSet)
	}

	if ws.persisted.SboxRepo != "" {
		err := os.RemoveAll(ws.persisted.SboxRepo)
		if err != nil {
			return err
		}
	}
	sbox, err := ws.createNewSandboxWithValidOrigin(ctx,
		ws.persisted.OriginRepo)
	if err != nil {
		return err
	}
	ws.persisted.SboxRepo = sbox

	return ws.Save()
}

func (ws *Workspace) AddOriginAndSandbox(ctx context.Context,
	originIn string) error {

	assert.NotEmpty(ws.persisted.ScratchDir)

	if originIn == "" {
		return fmt.Errorf("%w", ErrOriginRepoNotSet)
	}
	if ws.persisted.OriginRepo != "" {
		return fmt.Errorf("%w (%v)", ErrWorkspaceOriginAlreadySet, ws.persisted.OriginRepo)
	}
	if ws.persisted.SboxRepo != "" {
		return fmt.Errorf("%w (%v)", ErrWorkspaceSandboxAlreadySet, ws.persisted.SboxRepo)
	}

	originDir, sboxDir, err := ws.createNewSandbox(ctx, originIn)
	if err != nil {
		return err
	}

	ws.persisted.OriginRepo = originDir
	ws.persisted.SboxRepo = sboxDir
	if err := ws.Save(); err != nil {
		_ = os.RemoveAll(sboxDir)
		ws.persisted.OriginRepo = ""
		ws.persisted.SboxRepo = ""
		return err
	}

	return nil
}

// SandboxSyncStatus returns a RepoSyncStatus for the workspace sandbox.
//
// This is a small convenience wrapper so callers don't need to use the SCM
// client directly when the operation is conceptually "workspace"-scoped.
func (ws *Workspace) SandboxSyncStatus(ctx context.Context) (scm.RepoSyncStatus, error) {
	if ws.persisted.SboxRepo == "" {
		return scm.RepoSyncStatus{}, fmt.Errorf("%w", ErrWorkspaceNoSandboxSet)
	}
	return ws.scmClient.RepoSyncStatus(ctx, ws.persisted.SboxRepo)
}

func (ws *Workspace) GetPwd(ctx context.Context) string {
	_ = ctx
	if ws.persisted.SboxRepo != "" {
		return ws.persisted.SboxRepo
	}

	if !ws.sandboxParentCreated {
		if err := ws.createSandboxParent(); err != nil {
			// If we can't ensure the sandbox parent exists, return an empty
			// pwd so callers can handle it as "no working directory".
			return ""
		}
	}

	// When no sandbox is set, return the sandbox parent directory
	dir, err := ws.getSandboxDir("")
	if err != nil {
		// Keep GetPwd side-effect-free; callers should handle empty results.
		return ""
	}
	return dir
}
