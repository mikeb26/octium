/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/scm"
	"github.com/negrel/assert"
)

func (ws *Workspace) validateScmRepoDir(ctx context.Context, label string, dir string) error {
	assert.NotEmpty(dir)

	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%v %w (%v): %w", label, ErrRepoDoesNotExist, dir, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("%v %w: %v", label, ErrRepoNotDirectory, dir)
	}
	if _, err := ws.scmClient.RepoStatusString(ctx, dir); err != nil {
		return fmt.Errorf("%v %w: %v: %w", label, ErrRepoNotScmRepository, dir, err)
	}
	return nil
}

func (ws *Workspace) validateSandboxOriginRemote(ctx context.Context, sboxRepoDir string, originRepoDir string) error {
	remotes, err := ws.scmClient.ListRemoteRepos(ctx, sboxRepoDir)
	if err != nil {
		return err
	}

	var originRemote *scm.RemoteRepo
	for i := range remotes {
		if remotes[i].Name == "origin" {
			originRemote = &remotes[i]
			break
		}
	}
	if originRemote == nil {
		return fmt.Errorf("%w: %v", ErrSandboxNoOriginRemote, sboxRepoDir)
	}

	matchFetch := false
	if originRemote.FetchURL != "" {
		matchFetch, err = remoteMatchesDir(originRemote.FetchURL, originRepoDir, sboxRepoDir)
		if err != nil {
			return err
		}
	}
	matchPush := false
	if originRemote.PushURL != "" {
		matchPush, err = remoteMatchesDir(originRemote.PushURL, originRepoDir, sboxRepoDir)
		if err != nil {
			return err
		}
	}
	if !matchFetch && !matchPush {
		return fmt.Errorf("%w: sboxRepo %v originRepo %v (origin.fetch=%v origin.push=%v)",
			ErrSandboxOriginMismatch, sboxRepoDir, originRepoDir, originRemote.FetchURL, originRemote.PushURL)
	}

	return nil
}

func remoteMatchesDir(originURL string, srcRepoDir string, dstRepoDir string) (bool, error) {
	if originURL == "" {
		return false, nil
	}

	srcAbs, err := filepath.Abs(filepath.Clean(srcRepoDir))
	if err != nil {
		return false, fmt.Errorf("%w %v: %w", ErrResolveSrcRepoDir, srcRepoDir, err)
	}

	// originURL can be a normal path (e.g. /home/me/repo) or a file URL
	// (e.g. file:///home/me/repo) for local clones.
	originPath := originURL
	if strings.HasPrefix(originURL, "file://") {
		u, err := url.Parse(originURL)
		if err != nil {
			return false, fmt.Errorf("%w %q: %w", ErrParseOriginURL, originURL, err)
		}
		originPath = u.Path
	} else if strings.Contains(originURL, "://") {
		// Non-file URL schemes can't reasonably be compared to a local directory.
		return false, fmt.Errorf("%w: %v", ErrOriginRemoteNotLocalPath, originURL)
	}

	// If the remote is a relative path, interpret it relative to dst repo.
	if originPath != "" && !filepath.IsAbs(originPath) {
		originPath = filepath.Join(dstRepoDir, originPath)
	}
	originAbs, err := filepath.Abs(filepath.Clean(originPath))
	if err != nil {
		return false, fmt.Errorf("%w %v: %w", ErrResolveOriginPath, originPath, err)
	}

	return originAbs == srcAbs, nil
}

func (ws *Workspace) createNewSandbox(ctx context.Context,
	originIn string) (string, string, error) {

	originDir, err := filepath.Abs(filepath.Clean(originIn))
	if err != nil {
		return "", "", fmt.Errorf("%w %v: %w", ErrResolveOriginDir, originIn, err)
	}

	if err := ws.validateScmRepoDir(ctx, "Origin", originDir); err != nil {
		return "", "", err
	}

	sboxDir, err := ws.createNewSandboxWithValidOrigin(ctx, originDir)
	if err != nil {
		return "", "", err
	}

	return originDir, sboxDir, nil
}

func (ws *Workspace) getSandboxDir(base string) (string, error) {
	usrN, err := user.Current()
	if err != nil {
		return "", err
	}

	return filepath.Join(internal.CliSandboxRepoHome, usrN.Username,
		ws.persisted.Id, base), nil
}

func (ws *Workspace) createNewSandboxWithValidOrigin(ctx context.Context,
	originDir string) (string, error) {

	base := filepath.Base(originDir)
	sboxDir, err := ws.getSandboxDir(base)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(sboxDir); err == nil {
		if st.IsDir() {
			return "", fmt.Errorf("%w: %v", ErrSandboxDirAlreadyExists, sboxDir)
		}
		return "", fmt.Errorf("%w: %v", ErrSandboxPathNotDirectory, sboxDir)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("%w %v: %w", ErrSandboxStatDir, sboxDir, err)
	}

	if err := ws.scmClient.CloneRepo(ctx, originDir, sboxDir); err != nil {
		_ = os.RemoveAll(sboxDir)
		return "", err
	}
	if err := ws.validateScmRepoDir(ctx, "Sandbox", sboxDir); err != nil {
		_ = os.RemoveAll(sboxDir)
		return "", err
	}
	err = ws.validateSandboxOriginRemote(ctx, sboxDir, originDir)
	if err != nil {
		_ = os.RemoveAll(sboxDir)
		return "", err
	}

	err = ws.scmClient.ShareRepo(ctx, sboxDir)
	if err != nil {
		_ = os.RemoveAll(sboxDir)
		return "", fmt.Errorf("%w: %w", ErrSandboxDirChmod, err)
	}
	err = fixupSharedDirPerms(sboxDir)
	if err != nil {
		_ = os.RemoveAll(sboxDir)
		return "", fmt.Errorf("%w: %w", ErrSandboxDirChmod, err)
	}

	return sboxDir, nil
}

func normalizedDirEqual(a string, b string) (bool, error) {
	aAbs, err := filepath.Abs(filepath.Clean(a))
	if err != nil {
		return false, fmt.Errorf("%w %v: %w", ErrResolvePath, a, err)
	}
	bAbs, err := filepath.Abs(filepath.Clean(b))
	if err != nil {
		return false, fmt.Errorf("%w %v: %w", ErrResolvePath, b, err)
	}
	return aAbs == bAbs, nil
}

// SyncSandbox updates the sandbox repository from its configured upstream.
//
// It is a non-interactive operation intended to keep the sandbox up-to-date
// while preserving local commits when possible.
func (ws *Workspace) SyncSandbox(ctx context.Context, merge bool) error {
	if ws.persisted.SboxRepo == "" {
		return fmt.Errorf("%w", ErrWorkspaceNoSandboxSet)
	}

	st, err := ws.scmClient.RepoSyncStatus(ctx, ws.persisted.SboxRepo)
	if err != nil {
		return err
	}
	if st.UpstreamRemote == "" || st.UpstreamBranch == "" {
		return fmt.Errorf("%w; refusing to sync", ErrSandboxNoUpstream)
	}
	// Fetch latest changes from upstream remote.
	if err := ws.scmClient.FetchRemoteRepo(ctx, ws.persisted.SboxRepo, st.UpstreamRemote); err != nil {
		return err
	}

	if !merge {
		return nil
	}

	if st.HasUncommittedChanges {
		return fmt.Errorf("%w (including untracked files); refusing to sync", ErrSandboxDirty)
	}
	if st.Operation != scm.RepoOperationNormal {
		return fmt.Errorf("%w (%v); refusing to sync", ErrSandboxInProgress, st.Operation)
	}

	// Merge upstream (may leave the repo in conflict state).
	//
	// We intentionally do not specify a remote/branch here; SCM implementations
	// should use the branch's configured upstream.
	err = ws.scmClient.Merge(ctx, ws.persisted.SboxRepo, "", "")
	if err != nil {
		return err
	}
	// best effort; chmod can fail on files we dont own
	_ = fixupSharedDirPerms(ws.persisted.SboxRepo)

	return nil
}

// PushSandbox pushes commited changes from the the sandbox repository to the
// workspace's origin
func (ws *Workspace) PushSandbox(ctx context.Context, dstBranch string) error {
	if ws.persisted.SboxRepo == "" {
		return fmt.Errorf("%w", ErrWorkspaceNoSandboxSet)
	}

	// Validate the origin repository state before pushing (the push will target
	// the sandbox's configured "origin" remote).
	if ws.persisted.OriginRepo == "" {
		return fmt.Errorf("%w (push)", ErrWorkspaceNoOriginSet)
	}

	st, err := ws.scmClient.RepoSyncStatus(ctx, ws.persisted.OriginRepo)
	if err != nil {
		return err
	}
	if st.HasUncommittedChanges {
		return fmt.Errorf("%w (including untracked files); refusing to push", ErrOriginDirty)
	}
	if st.Operation != scm.RepoOperationNormal {
		return fmt.Errorf("%w (%v); refusing to push", ErrOriginInProgress, st.Operation)
	}

	// We intentionally do not specify a remote/branch here; SCM implementations
	// should use the branch's configured upstream.
	//
	// However: for workspace pushes we want to create/update dstBranch in the
	// origin repo even when the sandbox branch has no upstream configured.
	// Sandbox clones often do not have an upstream branch set.
	return ws.scmClient.Push(ctx, ws.persisted.SboxRepo, "origin", dstBranch)
}

// MergeSandbox merges committed changes from the workspace sandbox into the
// origin repository's currently checked out branch.
//
// The merge is performed in the origin repo by temporarily adding the sandbox
// repo as a remote, fetching it, and merging the remote's branch that matches
// the origin's current branch.
func (ws *Workspace) MergeSandbox(ctx context.Context) error {
	if ws.persisted.OriginRepo == "" {
		return fmt.Errorf("%w (merge)", ErrWorkspaceNoOriginSet)
	}
	if ws.persisted.SboxRepo == "" {
		return fmt.Errorf("%w", ErrWorkspaceNoSandboxSet)
	}

	originSt, err := ws.scmClient.RepoSyncStatus(ctx, ws.persisted.OriginRepo)
	if err != nil {
		return err
	}
	if originSt.HasUncommittedChanges {
		return fmt.Errorf("%w (including untracked files); refusing to merge", ErrOriginDirty)
	}
	if originSt.Operation != scm.RepoOperationNormal {
		return fmt.Errorf("%w (%v); refusing to merge", ErrOriginInProgress, originSt.Operation)
	}

	sboxSt, err := ws.scmClient.RepoSyncStatus(ctx, ws.persisted.SboxRepo)
	if err != nil {
		return err
	}
	if sboxSt.HasUncommittedChanges {
		return fmt.Errorf("%w (including untracked files); refusing to merge", ErrSandboxDirty)
	}
	if sboxSt.Operation != scm.RepoOperationNormal {
		return fmt.Errorf("%w (%v); refusing to merge", ErrSandboxInProgress, sboxSt.Operation)
	}

	remoteName, err := randRemoteName()
	if err != nil {
		return err
	}
	if err := ws.scmClient.AddRemoteRepo(ctx, ws.persisted.OriginRepo, remoteName, ws.persisted.SboxRepo); err != nil {
		return err
	}
	defer ws.scmClient.DeleteRemoteRepo(ctx, ws.persisted.OriginRepo, remoteName)

	if err := ws.scmClient.FetchRemoteRepo(ctx, ws.persisted.OriginRepo, remoteName); err != nil {
		return err
	}
	if strings.TrimSpace(sboxSt.LocalBranch) == "" {
		return fmt.Errorf("%w; refusing to merge", scm.ErrBranchRequired)
	}

	return ws.scmClient.Merge(ctx, ws.persisted.OriginRepo, remoteName, sboxSt.LocalBranch)
}

// CommitSandbox commits uncommitted local changes in the workspace sandbox
func (ws *Workspace) CommitSandbox(ctx context.Context,
	opts scm.CommitOptions) (*scm.UntrackedFiles, error) {

	untracked, err := ws.scmClient.Commit(ctx, ws.Sandbox(), opts)
	if err != nil {
		return untracked, err
	}

	// best effort; chmod can fail on files we dont own
	_ = fixupSharedDirPerms(ws.Sandbox())

	return untracked, nil
}

func randRemoteName() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// 4 bytes => 8 hex characters.
	return internal.CliToolName + "_" + hex.EncodeToString(b[:]), nil
}
