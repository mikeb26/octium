/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/scm"
	"github.com/stretchr/testify/assert"
)

func TestWorkspace_Save_WritesWorkspaceFile(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()

	c := &fakeSCMClient{}
	ws := New(scratchDir, "test", c)
	ws.persisted.OriginRepo = "/origin"
	ws.persisted.SboxRepo = "/sbox"

	err := ws.Save()
	mustNoErr(t, err)

	bs, err := os.ReadFile(filepath.Join(scratchDir, WorkspaceFileName))
	mustNoErr(t, err)

	var got persistedWorkspace
	mustNoErr(t, json.Unmarshal(bs, &got))
	assert.Equal(t, scratchDir, got.ScratchDir)
	assert.Equal(t, "/origin", got.OriginRepo)
	assert.Equal(t, "/sbox", got.SboxRepo)
}

func TestWorkspace_Load_ErrorsWhenScratchDirNotSet(t *testing.T) {
	ws := &Workspace{}
	err := ws.Load(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scratch dir")
}

func TestWorkspace_Load_CreatesFileWhenMissing(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	ws := New(scratchDir, "test", &fakeSCMClient{})

	err := ws.Load(context.Background())
	mustNoErr(t, err)
	_, statErr := os.Stat(filepath.Join(scratchDir, WorkspaceFileName))
	mustNoErr(t, statErr)
}

func TestWorkspace_Load_ErrorsOnInvalidJSON(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	ws := New(scratchDir, "test", &fakeSCMClient{})
	mustNoErr(t, os.WriteFile(filepath.Join(scratchDir, WorkspaceFileName), []byte("not-json"), 0o600))

	err := ws.Load(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestWorkspace_Load_ErrorsOnScratchDirMismatch(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	ws := New(scratchDir, "test", &fakeSCMClient{})

	bad := persistedWorkspace{ScratchDir: filepath.Join(t.TempDir(), "different")}
	bs := mustMarshal(t, &bad)
	mustNoErr(t, os.WriteFile(filepath.Join(scratchDir, WorkspaceFileName), bs, 0o600))

	err := ws.Load(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scratch dir mismatch")
}

func TestWorkspace_Load_SucceedsWithEmptyRepos(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	ws := New(scratchDir, "test", &fakeSCMClient{})

	loaded := persistedWorkspace{ScratchDir: scratchDir, OriginRepo: "", SboxRepo: ""}
	bs := mustMarshal(t, &loaded)
	mustNoErr(t, os.WriteFile(filepath.Join(scratchDir, WorkspaceFileName), bs, 0o600))

	err := ws.Load(context.Background())
	mustNoErr(t, err)
	assert.Equal(t, scratchDir, ws.persisted.ScratchDir)
	assert.Equal(t, "", ws.persisted.OriginRepo)
	assert.Equal(t, "", ws.persisted.SboxRepo)
}

func TestWorkspace_Load_ValidatesOriginRepoExistsAndIsRepo(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	originDir := filepath.Join(t.TempDir(), "origin")
	mustNoErr(t, os.MkdirAll(originDir, 0o700))

	c := &fakeSCMClient{
		repoStatusErr: map[string]error{},
		repoStatusStr: map[string]string{originDir: "ok"},
		listRemotes:   map[string][]scm.RemoteRepo{},
		listErr:       map[string]error{},
	}
	ws := New(scratchDir, "test", c)

	loaded := persistedWorkspace{ScratchDir: scratchDir, OriginRepo: originDir}
	bs := mustMarshal(t, &loaded)
	mustNoErr(t, os.WriteFile(filepath.Join(scratchDir, WorkspaceFileName), bs, 0o600))

	err := ws.Load(ctx)
	mustNoErr(t, err)
	assert.Equal(t, originDir, ws.persisted.OriginRepo)
}

func TestWorkspace_Load_ErrorsWhenOriginRepoMissing(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	missingOrigin := filepath.Join(t.TempDir(), "missing")

	ws := New(scratchDir, "test", &fakeSCMClient{})
	loaded := persistedWorkspace{ScratchDir: scratchDir, OriginRepo: missingOrigin}
	bs := mustMarshal(t, &loaded)
	mustNoErr(t, os.WriteFile(filepath.Join(scratchDir, WorkspaceFileName), bs, 0o600))

	err := ws.Load(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestWorkspace_Load_ErrorsWhenOriginRepoNotDir(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	originFile := filepath.Join(t.TempDir(), "origin_file")
	mustNoErr(t, os.WriteFile(originFile, []byte("x"), 0o600))

	ws := New(scratchDir, "test", &fakeSCMClient{})
	loaded := persistedWorkspace{ScratchDir: scratchDir, OriginRepo: originFile}
	bs := mustMarshal(t, &loaded)
	mustNoErr(t, os.WriteFile(filepath.Join(scratchDir, WorkspaceFileName), bs, 0o600))

	err := ws.Load(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestWorkspace_Load_ErrorsWhenOriginRepoNotScmRepo(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	originDir := filepath.Join(t.TempDir(), "origin")
	mustNoErr(t, os.MkdirAll(originDir, 0o700))

	c := &fakeSCMClient{
		repoStatusErr: map[string]error{originDir: errors.New("not a repo")},
		repoStatusStr: map[string]string{},
		listRemotes:   map[string][]scm.RemoteRepo{},
		listErr:       map[string]error{},
	}
	ws := New(scratchDir, "test", c)

	loaded := persistedWorkspace{ScratchDir: scratchDir, OriginRepo: originDir}
	bs := mustMarshal(t, &loaded)
	mustNoErr(t, os.WriteFile(filepath.Join(scratchDir, WorkspaceFileName), bs, 0o600))

	err := ws.Load(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not an scm repository")
}

func TestWorkspace_Load_ErrorsWhenSboxSetButOriginEmpty(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	sboxDir := filepath.Join(t.TempDir(), "sbox")
	mustNoErr(t, os.MkdirAll(sboxDir, 0o700))

	ws := New(scratchDir, "test", &fakeSCMClient{})
	loaded := persistedWorkspace{ScratchDir: scratchDir, OriginRepo: "", SboxRepo: sboxDir}
	bs := mustMarshal(t, &loaded)
	mustNoErr(t, os.WriteFile(filepath.Join(scratchDir, WorkspaceFileName), bs, 0o600))

	err := ws.Load(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox is set")
}

func TestWorkspace_Load_SucceedsWithOriginAndSandboxAndRemoteMatch(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()
	root := t.TempDir()
	originDir := filepath.Join(root, "origin")
	sboxDir := filepath.Join(root, "sbox")
	mustNoErr(t, os.MkdirAll(originDir, 0o700))
	mustNoErr(t, os.MkdirAll(sboxDir, 0o700))

	fetchURL := "file://" + originDir
	c := &fakeSCMClient{
		repoStatusErr: map[string]error{},
		repoStatusStr: map[string]string{originDir: "ok", sboxDir: "ok"},
		listRemotes: map[string][]scm.RemoteRepo{
			sboxDir: {{Name: "origin", FetchURL: fetchURL}},
		},
		listErr: map[string]error{},
	}
	ws := New(scratchDir, "test", c)

	loaded := persistedWorkspace{ScratchDir: scratchDir, OriginRepo: originDir, SboxRepo: sboxDir}
	bs := mustMarshal(t, &loaded)
	mustNoErr(t, os.WriteFile(filepath.Join(scratchDir, WorkspaceFileName), bs, 0o600))

	err := ws.Load(ctx)
	mustNoErr(t, err)
	assert.Equal(t, originDir, ws.persisted.OriginRepo)
	assert.Equal(t, sboxDir, ws.persisted.SboxRepo)
}

func TestWorkspace_GetPwd_CreatesSandboxParentDir(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHomeBase := internal.CliSandboxRepoHomeBase
	internal.CliSandboxRepoHomeBase = t.TempDir()
	defer func() { internal.CliSandboxRepoHomeBase = prevSandboxRepoHomeBase }()

	ws := New(scratchDir, "test", &fakeSCMClient{})
	mustNoErr(t, ws.Save())
	_ = ws.GetPwd(context.Background())

	parentDir, err := ws.getSandboxDir("")
	mustNoErr(t, err)
	st, err := os.Stat(parentDir)
	mustNoErr(t, err)
	assert.True(t, st.IsDir())
}
