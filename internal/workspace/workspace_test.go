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

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	bs, err := json.Marshal(v)
	mustNoErr(t, err)
	return bs
}

func TestNew_InitializesScratchDir(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	c := &fakeSCMClient{}

	ws := New(scratchDir, "test", c)
	if ws == nil {
		t.Fatalf("expected workspace")
	}
	assert.Equal(t, scratchDir, ws.persisted.ScratchDir)
	assert.Equal(t, "", ws.persisted.OriginRepo)
	assert.Equal(t, "", ws.persisted.SboxRepo)
}

func TestWorkspace_Detail_FormatsFields(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	c := &fakeSCMClient{}
	ws := New(scratchDir, "test", c)
	ws.persisted.OriginRepo = "/tmp/origin"
	ws.persisted.SboxRepo = "/tmp/sbox"

	d := ws.Detail()
	assert.Contains(t, d, "Scratch:")
	assert.Contains(t, d, scratchDir)
	assert.Contains(t, d, "Origin:")
	assert.Contains(t, d, "/tmp/origin")
	assert.Contains(t, d, "Sandbox:")
	assert.Contains(t, d, "/tmp/sbox")
}

func TestWorkspace_String_EmptyOrigin_ReturnsEmpty(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	c := &fakeSCMClient{}
	ws := New(scratchDir, "test", c)

	s := ws.String(context.Background())
	assert.Equal(t, "", s)
}

func TestWorkspace_String_Statuses(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	origin := filepath.Join(t.TempDir(), "origin")
	sbox := filepath.Join(t.TempDir(), "sbox")

	c := &fakeSCMClient{
		repoStatusErr: map[string]error{},
		repoStatusStr: map[string]string{
			origin: "clean",
			sbox:   "dirty",
		},
		listRemotes: map[string][]scm.RemoteRepo{},
		listErr:     map[string]error{},
	}
	ws := New(scratchDir, "test", c)
	ws.persisted.OriginRepo = origin
	ws.persisted.SboxRepo = sbox

	s := ws.String(ctx)
	assert.Equal(t, "Origin:[clean] Sandbox:[dirty]", s)
}

func TestWorkspace_String_UnknownStatusOnError(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	origin := filepath.Join(t.TempDir(), "origin")
	sbox := filepath.Join(t.TempDir(), "sbox")

	c := &fakeSCMClient{
		repoStatusErr: map[string]error{
			origin: errors.New("boom"),
			sbox:   errors.New("boom"),
		},
		repoStatusStr: map[string]string{},
		listRemotes:   map[string][]scm.RemoteRepo{},
		listErr:       map[string]error{},
	}
	ws := New(scratchDir, "test", c)
	ws.persisted.OriginRepo = origin
	ws.persisted.SboxRepo = sbox

	s := ws.String(ctx)
	assert.Equal(t, "Origin:[<unknown>] Sandbox:[<unknown>]", s)
}

func TestWorkspace_Destroy_RemovesSandboxAndClearsRepos(t *testing.T) {
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	sboxDir := filepath.Join(t.TempDir(), "sbox")
	mustNoErr(t, os.MkdirAll(sboxDir, 0o700))
	mustNoErr(t, os.WriteFile(filepath.Join(sboxDir, "file.txt"), []byte("x"), 0o600))

	c := &fakeSCMClient{}
	ws := New(scratchDir, "test", c)
	ws.persisted.OriginRepo = "/some/origin"
	ws.persisted.SboxRepo = sboxDir

	err := ws.Destroy()
	mustNoErr(t, err)
	assert.Equal(t, "", ws.persisted.OriginRepo)
	assert.Equal(t, "", ws.persisted.SboxRepo)
	_, statErr := os.Stat(sboxDir)
	assert.Error(t, statErr)
	assert.True(t, os.IsNotExist(statErr))
}

func TestWorkspace_AddOriginAndSandbox_SetsOriginAndClonesSandboxAndSaves(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()

	originDir := filepath.Join(t.TempDir(), "origin")
	mustNoErr(t, os.MkdirAll(originDir, 0o700))

	c := &fakeSCMClient{}
	ws := New(scratchDir, "test", c)

	sboxDir, err := ws.getSandboxDir(filepath.Base(originDir))
	mustNoErr(t, err)

	c.repoStatusErr = map[string]error{}
	c.repoStatusStr = map[string]string{originDir: "ok", sboxDir: "ok"}
	c.listRemotes = map[string][]scm.RemoteRepo{
		sboxDir: {{Name: "origin", FetchURL: "file://" + originDir}},
	}
	c.listErr = map[string]error{}

	err = ws.AddOriginAndSandbox(ctx, originDir)
	mustNoErr(t, err)
	assert.Equal(t, originDir, ws.Origin())
	assert.Equal(t, sboxDir, ws.Sandbox())
	assert.Len(t, c.cloneCalls, 1)
	assert.Equal(t, originDir, c.cloneCalls[0].src)
	assert.Equal(t, sboxDir, c.cloneCalls[0].dst)
	assert.Equal(t, []string{sboxDir}, c.shareCalls)

	// ensure ws.Save() happened
	_, statErr := os.Stat(filepath.Join(scratchDir, WorkspaceFileName))
	mustNoErr(t, statErr)
}

func TestWorkspace_SyncSandbox_ErrorsWhenNoSandbox(t *testing.T) {
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	ws := New(t.TempDir(), "test", &fakeSCMClient{})
	err := ws.SyncSandbox(context.Background(), true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox")
}

func TestWorkspace_SyncSandbox_ErrorsWhenNoUpstreamConfigured(t *testing.T) {
	sbox := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	c := &fakeSCMClient{
		syncStatus: map[string]scm.RepoSyncStatus{
			sbox: {UpstreamRemote: "", UpstreamBranch: ""},
		},
	}
	ws := New(t.TempDir(), "test", c)
	ws.persisted.SboxRepo = sbox
	err := ws.SyncSandbox(context.Background(), true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no upstream")
}

func TestWorkspace_SyncSandbox_ErrorsOnDirtyRepo(t *testing.T) {
	sbox := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	c := &fakeSCMClient{
		syncStatus: map[string]scm.RepoSyncStatus{
			sbox: {UpstreamRemote: "origin", UpstreamBranch: "main", HasUncommittedChanges: true, Operation: scm.RepoOperationNormal},
		},
	}
	ws := New(t.TempDir(), "test", c)
	ws.persisted.SboxRepo = sbox
	err := ws.SyncSandbox(context.Background(), true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted")
}

func TestWorkspace_SyncSandbox_ErrorsOnNonNormalOperation(t *testing.T) {
	sbox := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	c := &fakeSCMClient{
		syncStatus: map[string]scm.RepoSyncStatus{
			sbox: {UpstreamRemote: "origin", UpstreamBranch: "main", HasUncommittedChanges: false, Operation: scm.RepoOperationRebasing},
		},
	}
	ws := New(t.TempDir(), "test", c)
	ws.persisted.SboxRepo = sbox
	err := ws.SyncSandbox(context.Background(), true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "in-progress")
}

func TestWorkspace_SyncSandbox_FetchesAndMerges(t *testing.T) {
	sbox := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	c := &fakeSCMClient{
		syncStatus: map[string]scm.RepoSyncStatus{
			sbox: {UpstreamRemote: "origin", UpstreamBranch: "main", HasUncommittedChanges: false, Operation: scm.RepoOperationNormal},
		},
	}
	ws := New(t.TempDir(), "test", c)
	ws.persisted.SboxRepo = sbox
	err := ws.SyncSandbox(context.Background(), true)
	assert.NoError(t, err)
	assert.Len(t, c.fetchCalls, 1)
	assert.Equal(t, sbox, c.fetchCalls[0].dir)
	assert.Equal(t, "origin", c.fetchCalls[0].remote)
	assert.Equal(t, []mergeCall{{dir: sbox, remote: "", branch: ""}}, c.mergeCalls)
}

func TestWorkspace_SyncSandbox_ReportsMergeConflicts(t *testing.T) {
	sbox := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	c := &fakeSCMClient{
		syncStatus: map[string]scm.RepoSyncStatus{
			sbox: {UpstreamRemote: "origin", UpstreamBranch: "main", HasUncommittedChanges: false, Operation: scm.RepoOperationNormal},
		},
		mergeErr: map[string]error{sbox: scm.ErrMergeConflicts},
	}
	ws := New(t.TempDir(), "test", c)
	ws.persisted.SboxRepo = sbox
	err := ws.SyncSandbox(context.Background(), true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts")
}

func TestWorkspace_GetPwd_ReturnsSandboxWhenSet(t *testing.T) {
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()

	ws := New(t.TempDir(), "test", &fakeSCMClient{})
	ws.persisted.SboxRepo = "/tmp/sbox"
	assert.Equal(t, "/tmp/sbox", ws.GetPwd(context.Background()))
}

func TestWorkspace_GetPwd_ReturnsSandboxParentWhenSandboxNotSet(t *testing.T) {
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()

	ws := New(t.TempDir(), "test", &fakeSCMClient{})
	parent, err := ws.getSandboxDir("")
	mustNoErr(t, err)
	assert.Equal(t, parent, ws.GetPwd(context.Background()))
}
