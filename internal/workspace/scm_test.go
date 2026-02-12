/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/scm"
	"github.com/stretchr/testify/assert"
)

type fakeSCMClient struct {
	repoStatusErr  map[string]error
	repoStatusStr  map[string]string
	listRemotes    map[string][]scm.RemoteRepo
	listErr        map[string]error
	addRemoteErr   map[string]error
	addRemoteCalls []addRemoteCall
	cloneErr       map[string]error
	cloneCalls     []cloneCall
	syncStatus     map[string]scm.RepoSyncStatus
	mergeErr       map[string]error
	pushErr        map[string]error
	fetchCalls     []fetchCall
	mergeCalls     []mergeCall
	pushCalls      []pushCall
	shareCalls     []string
	shareErr       map[string]error
}

type cloneCall struct {
	src string
	dst string
}

type fetchCall struct {
	dir    string
	remote string
}

type addRemoteCall struct {
	dir        string
	remoteName string
	remoteURL  string
}

type mergeCall struct {
	dir    string
	remote string
	branch string
}

type pushCall struct {
	dir    string
	remote string
	branch string
}

func (f *fakeSCMClient) RepoStatusString(ctx context.Context, dir string) (string, error) {
	_ = ctx
	if err, ok := f.repoStatusErr[dir]; ok && err != nil {
		return "", err
	}
	if s, ok := f.repoStatusStr[dir]; ok {
		return s, nil
	}
	return "", fmt.Errorf("unknown repo: %v", dir)
}

func (f *fakeSCMClient) InitRepo(ctx context.Context, dstDir string) error {
	_ = ctx
	_ = dstDir
	return errors.New("not implemented")
}

func (f *fakeSCMClient) CloneRepo(ctx context.Context, srcRepoURL string, dstDir string) error {
	_ = ctx
	if f.cloneErr != nil {
		if err, ok := f.cloneErr[dstDir]; ok && err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return err
	}
	f.cloneCalls = append(f.cloneCalls, cloneCall{src: srcRepoURL, dst: dstDir})
	return nil
}

func (f *fakeSCMClient) ShareRepo(ctx context.Context, dir string) error {
	_ = ctx
	f.shareCalls = append(f.shareCalls, dir)
	if f.shareErr != nil {
		if err, ok := f.shareErr[dir]; ok {
			return err
		}
	}
	return nil
}

func (f *fakeSCMClient) ListRemoteRepos(ctx context.Context, dir string) ([]scm.RemoteRepo, error) {
	_ = ctx
	if err, ok := f.listErr[dir]; ok && err != nil {
		return nil, err
	}
	if r, ok := f.listRemotes[dir]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("unknown repo for remotes: %v", dir)
}

func (f *fakeSCMClient) AddRemoteRepo(ctx context.Context, dir string, remoteName string, remoteURL string) error {
	_ = ctx
	f.addRemoteCalls = append(f.addRemoteCalls, addRemoteCall{dir: dir, remoteName: remoteName, remoteURL: remoteURL})
	if f.addRemoteErr != nil {
		if err, ok := f.addRemoteErr[dir]; ok && err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeSCMClient) DeleteRemoteRepo(ctx context.Context, dir string, remoteName string) error {
	_ = ctx
	_ = dir
	_ = remoteName
	return errors.New("not implemented")
}

func (f *fakeSCMClient) FetchRemoteRepo(ctx context.Context, dir string, remoteName string) error {
	_ = ctx
	f.fetchCalls = append(f.fetchCalls, fetchCall{dir: dir, remote: remoteName})
	return nil
}

func (f *fakeSCMClient) RepoSyncStatus(ctx context.Context, dir string) (scm.RepoSyncStatus, error) {
	_ = ctx
	if f.syncStatus != nil {
		if st, ok := f.syncStatus[dir]; ok {
			return st, nil
		}
	}
	return scm.RepoSyncStatus{}, errors.New("not implemented")
}

func (f *fakeSCMClient) Merge(ctx context.Context, dir string, remoteName string, branch string) error {
	_ = ctx
	f.mergeCalls = append(f.mergeCalls, mergeCall{dir: dir, remote: remoteName, branch: branch})
	if f.mergeErr != nil {
		if err, ok := f.mergeErr[dir]; ok {
			return err
		}
	}
	return nil
}

func (f *fakeSCMClient) Push(ctx context.Context, dir string, remoteName string, branch string) error {
	_ = ctx
	f.pushCalls = append(f.pushCalls, pushCall{dir: dir, remote: remoteName, branch: branch})
	if f.pushErr != nil {
		if err, ok := f.pushErr[dir]; ok {
			return err
		}
	}
	return nil
}

func (f *fakeSCMClient) ListBranches(ctx context.Context, dir string, remoteName string) ([]string, error) {
	_ = ctx
	_ = dir
	_ = remoteName
	return nil, errors.New("not implemented")
}

func (f *fakeSCMClient) DiffTool(ctx context.Context, dir string, spec scm.DiffSpec) error {
	_ = ctx
	_ = dir
	_ = spec
	return errors.New("not implemented")
}

func (f *fakeSCMClient) Commit(ctx context.Context, dir string, opts scm.CommitOptions) (*scm.UntrackedFiles, error) {
	_ = ctx
	_ = dir
	_ = opts
	return nil, errors.New("not implemented")
}

func TestWorkspace_MergeSandbox_AddsRemoteFetchesAndMergesIntoOriginBranch(t *testing.T) {
	ctx := context.Background()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	origin := t.TempDir()
	sbox := t.TempDir()

	c := &fakeSCMClient{
		syncStatus: map[string]scm.RepoSyncStatus{
			origin: {HasUncommittedChanges: false, Operation: scm.RepoOperationNormal},
			sbox:   {LocalBranch: "main", HasUncommittedChanges: false, Operation: scm.RepoOperationNormal},
		},
	}
	ws := New(t.TempDir(), "test", c)
	ws.persisted.OriginRepo = origin
	ws.persisted.SboxRepo = sbox

	err := ws.MergeSandbox(ctx)
	assert.NoError(t, err)
	assert.Len(t, c.addRemoteCalls, 1)
	assert.Equal(t, origin, c.addRemoteCalls[0].dir)
	assert.Equal(t, sbox, c.addRemoteCalls[0].remoteURL)
	assert.True(t, strings.HasPrefix(c.addRemoteCalls[0].remoteName, internal.CliToolName+"_"))
	assert.Equal(t, 7+8, len(c.addRemoteCalls[0].remoteName))

	assert.Len(t, c.fetchCalls, 1)
	assert.Equal(t, origin, c.fetchCalls[0].dir)
	assert.Equal(t, c.addRemoteCalls[0].remoteName, c.fetchCalls[0].remote)

	assert.Equal(t, []mergeCall{{dir: origin, remote: c.addRemoteCalls[0].remoteName, branch: "main"}}, c.mergeCalls)
}

func TestWorkspace_MergeSandbox_ErrorsWhenOriginDirty(t *testing.T) {
	ctx := context.Background()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	origin := t.TempDir()
	sbox := t.TempDir()

	c := &fakeSCMClient{
		syncStatus: map[string]scm.RepoSyncStatus{
			origin: {HasUncommittedChanges: true, Operation: scm.RepoOperationNormal},
			sbox:   {LocalBranch: "main", HasUncommittedChanges: false, Operation: scm.RepoOperationNormal},
		},
	}
	ws := New(t.TempDir(), "test", c)
	ws.persisted.OriginRepo = origin
	ws.persisted.SboxRepo = sbox

	err := ws.MergeSandbox(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "origin has uncommitted changes")
}

func TestWorkspace_MergeSandbox_ErrorsWhenSandboxDirty(t *testing.T) {
	ctx := context.Background()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	origin := t.TempDir()
	sbox := t.TempDir()

	c := &fakeSCMClient{
		syncStatus: map[string]scm.RepoSyncStatus{
			origin: {HasUncommittedChanges: false, Operation: scm.RepoOperationNormal},
			sbox:   {LocalBranch: "main", HasUncommittedChanges: true, Operation: scm.RepoOperationNormal},
		},
	}
	ws := New(t.TempDir(), "test", c)
	ws.persisted.OriginRepo = origin
	ws.persisted.SboxRepo = sbox

	err := ws.MergeSandbox(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox has uncommitted changes")
}

func TestWorkspace_validateScmRepoDir_SucceedsWhenDirAndRepo(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	repoDir := filepath.Join(t.TempDir(), "repo")
	mustNoErr(t, os.MkdirAll(repoDir, 0o700))

	c := &fakeSCMClient{
		repoStatusErr: map[string]error{},
		repoStatusStr: map[string]string{repoDir: "ok"},
		listRemotes:   map[string][]scm.RemoteRepo{},
		listErr:       map[string]error{},
	}
	ws := New(scratchDir, "test", c)
	mustNoErr(t, ws.validateScmRepoDir(ctx, "X", repoDir))
}

func TestWorkspace_validateSandboxOriginRemote_ErrorsWhenNoOriginRemote(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	originDir := filepath.Join(t.TempDir(), "origin")
	sboxDir := filepath.Join(t.TempDir(), "sbox")
	mustNoErr(t, os.MkdirAll(originDir, 0o700))
	mustNoErr(t, os.MkdirAll(sboxDir, 0o700))

	c := &fakeSCMClient{
		repoStatusErr: map[string]error{},
		repoStatusStr: map[string]string{originDir: "ok", sboxDir: "ok"},
		listRemotes: map[string][]scm.RemoteRepo{
			sboxDir: {{Name: "upstream", FetchURL: originDir}},
		},
		listErr: map[string]error{},
	}
	ws := New(scratchDir, "test", c)

	err := ws.validateSandboxOriginRemote(ctx, sboxDir, originDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no 'origin' remote")
}

func TestWorkspace_validateSandboxOriginRemote_SucceedsOnFetchMatch(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	root := t.TempDir()
	originDir := filepath.Join(root, "origin")
	sboxDir := filepath.Join(root, "sbox")
	mustNoErr(t, os.MkdirAll(originDir, 0o700))
	mustNoErr(t, os.MkdirAll(sboxDir, 0o700))

	// Make a relative remote path from sbox -> origin.
	rel, err := filepath.Rel(sboxDir, originDir)
	mustNoErr(t, err)

	c := &fakeSCMClient{
		repoStatusErr: map[string]error{},
		repoStatusStr: map[string]string{originDir: "ok", sboxDir: "ok"},
		listRemotes: map[string][]scm.RemoteRepo{
			sboxDir: {{Name: "origin", FetchURL: rel}},
		},
		listErr: map[string]error{},
	}
	ws := New(scratchDir, "test", c)
	mustNoErr(t, ws.validateSandboxOriginRemote(ctx, sboxDir, originDir))
}

func TestWorkspace_validateSandboxOriginRemote_SucceedsOnPushMatch(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	originDir := filepath.Join(t.TempDir(), "origin")
	sboxDir := filepath.Join(t.TempDir(), "sbox")
	mustNoErr(t, os.MkdirAll(originDir, 0o700))
	mustNoErr(t, os.MkdirAll(sboxDir, 0o700))

	c := &fakeSCMClient{
		repoStatusErr: map[string]error{},
		repoStatusStr: map[string]string{originDir: "ok", sboxDir: "ok"},
		listRemotes: map[string][]scm.RemoteRepo{
			sboxDir: {{Name: "origin", FetchURL: "", PushURL: originDir}},
		},
		listErr: map[string]error{},
	}
	ws := New(scratchDir, "test", c)
	mustNoErr(t, ws.validateSandboxOriginRemote(ctx, sboxDir, originDir))
}

func TestWorkspace_validateSandboxOriginRemote_ErrorsOnNonLocalRemoteURL(t *testing.T) {
	ctx := context.Background()
	scratchDir := t.TempDir()
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	originDir := filepath.Join(t.TempDir(), "origin")
	sboxDir := filepath.Join(t.TempDir(), "sbox")
	mustNoErr(t, os.MkdirAll(originDir, 0o700))
	mustNoErr(t, os.MkdirAll(sboxDir, 0o700))

	c := &fakeSCMClient{
		repoStatusErr: map[string]error{},
		repoStatusStr: map[string]string{originDir: "ok", sboxDir: "ok"},
		listRemotes: map[string][]scm.RemoteRepo{
			sboxDir: {{Name: "origin", FetchURL: "https://example.com/repo"}},
		},
		listErr: map[string]error{},
	}
	ws := New(scratchDir, "test", c)

	err := ws.validateSandboxOriginRemote(ctx, sboxDir, originDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a local path")
}

func TestRemoteMatchesDir_AbsolutePath(t *testing.T) {
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mustNoErr(t, os.MkdirAll(src, 0o700))
	mustNoErr(t, os.MkdirAll(dst, 0o700))

	ok, err := remoteMatchesDir(src, src, dst)
	mustNoErr(t, err)
	assert.True(t, ok)
}

func TestRemoteMatchesDir_RelativePathInterpretedFromDst(t *testing.T) {
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mustNoErr(t, os.MkdirAll(src, 0o700))
	mustNoErr(t, os.MkdirAll(dst, 0o700))

	rel, err := filepath.Rel(dst, src)
	mustNoErr(t, err)

	ok, err := remoteMatchesDir(rel, src, dst)
	mustNoErr(t, err)
	assert.True(t, ok)
}

func TestRemoteMatchesDir_FileURL(t *testing.T) {
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mustNoErr(t, os.MkdirAll(src, 0o700))
	mustNoErr(t, os.MkdirAll(dst, 0o700))

	ok, err := remoteMatchesDir("file://"+src, src, dst)
	mustNoErr(t, err)
	assert.True(t, ok)
}

func TestRemoteMatchesDir_NonFileSchemeErrors(t *testing.T) {
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mustNoErr(t, os.MkdirAll(src, 0o700))
	mustNoErr(t, os.MkdirAll(dst, 0o700))

	_, err := remoteMatchesDir("https://example.com/repo", src, dst)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a local path")
}

func TestNormalizedDirEqual_NormalizesPaths(t *testing.T) {
	prevSandboxRepoHome := internal.CliSandboxRepoHome
	internal.CliSandboxRepoHome = t.TempDir()
	defer func() { internal.CliSandboxRepoHome = prevSandboxRepoHome }()
	root := t.TempDir()
	a := filepath.Join(root, "a")
	mustNoErr(t, os.MkdirAll(a, 0o700))

	ok, err := normalizedDirEqual(a+string(os.PathSeparator), filepath.Join(root, "a"))
	mustNoErr(t, err)
	assert.True(t, ok)
}
