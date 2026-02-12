/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

// Package scm defines a small, VCS-agnostic abstraction for source control
// operations
package scm

import "context"

// RepoOperation describes whether the repo is in a state that makes it unsafe
// to perform automated sync operations.
//
// Values are SCM-agnostic; each SCM backend maps its own in-progress
// operations into this set.
type RepoOperation int

const (
	RepoOperationNormal RepoOperation = iota
	RepoOperationMerging
	RepoOperationRebasing
	RepoOperationCherryPicking
	RepoOperationReverting
	RepoOperationBisecting
	RepoOperationOther
)

func (o RepoOperation) String() string {
	switch o {
	case RepoOperationNormal:
		return "normal"
	case RepoOperationMerging:
		return "merging"
	case RepoOperationRebasing:
		return "rebasing"
	case RepoOperationCherryPicking:
		return "cherry-picking"
	case RepoOperationReverting:
		return "reverting"
	case RepoOperationBisecting:
		return "bisecting"
	case RepoOperationOther:
		return "other"
	default:
		return "unknown"
	}
}

// RepoSyncStatus provides SCM-agnostic repository status required for
// non-interactive syncing.
type RepoSyncStatus struct {
	// LocalBranch is the currently checked-out local branch.
	//
	// When the repo is in a detached HEAD state or the underlying SCM does not
	// expose a concept of a local branch, this may be empty.
	LocalBranch string

	// UpstreamRemote and UpstreamBranch represent the configured upstream
	// of the current branch.
	//
	// When no upstream is configured, both values are empty.
	UpstreamRemote string
	UpstreamBranch string

	// Ahead/Behind are relative to upstream.
	Ahead  int
	Behind int

	// HasUncommittedChanges is true when there are staged, unstaged, or untracked
	// changes.
	HasUncommittedChanges bool

	// Operation describes any in-progress operation (rebase, cherry-pick, etc).
	Operation RepoOperation
}

// RemoteRepo represents a configured remote in a repository.
//
// It is equivalent to the information exposed by `git remote -v`.
//
// FetchURL and PushURL may be empty when the underlying SCM does not expose
// both URLs or when they are not configured.
type RemoteRepo struct {
	Name     string
	FetchURL string
	PushURL  string
}

// CommitOptions controls the behavior of Client.Commit.
type CommitOptions struct {
	// IncludeUntracked lists all untracked files currently present in the repo
	// and whether each should be included in the commit.
	//
	// For every untracked file present in the repo at commit time, this map must
	// contain a key for that file:
	//   - true  => stage/include the file in the commit
	//   - false => do not stage/include the file in the commit
	//
	// If any untracked files are present and not mentioned in this map, Commit
	// will return an UntrackedFilesError so callers can prompt the user and
	// retry.
	IncludeUntracked map[string]bool

	// Message, when non-empty, triggers a non-interactive commit.
	//
	// In this mode, the implementation should avoid invoking an editor or
	// prompting on stdin (e.g. by using `git commit -m <message>`), and it may
	// apply client-level timeouts for low-latency automated flows.
	//
	// When empty, Commit should behave interactively (e.g. `git commit` and let
	// git invoke the user's configured editor).
	Message string
}

// UntrackedFiles indicates that set of untracked files that are present and
// which should be accounted for within CommitOptions.IncludeUntracked in order
// for a commit to proceed successfully
type UntrackedFiles struct {
	Filename []string
}

// DiffScope describes which set of changes should be presented by DiffTool.
type DiffScope int

const (
	// DiffScopeUncommitted shows all uncommitted changes (both staged and
	// unstaged).
	DiffScopeUncommitted DiffScope = iota

	// DiffScopeBranchUpstream shows differences between a branch and its
	// configured upstream.
	//
	// For Git, this is the equivalent of:
	//   git difftool <upstream> <branch>
	DiffScopeBranchUpstream
)

// DiffSpec specifies which changes should be presented by DiffTool.
type DiffSpec struct {
	Scope DiffScope

	// Branch selects which branch to diff for branch-based scopes.
	//
	// When empty, implementations should interpret this as the currently
	// checked-out branch.
	Branch string
}

// Client is a VCS-agnostic client for the small set of source-control
// operations needed by the UI.
type Client interface {
	RepoStatusString(ctx context.Context, dir string) (string, error)
	InitRepo(ctx context.Context, dstDir string) error
	CloneRepo(ctx context.Context, srcRepoURL string, dstDir string) error

	// ShareRepo configures a repository so it can be shared across users.
	//
	// For Git, this sets core.sharedRepository.
	ShareRepo(ctx context.Context, dir string) error

	// RepoSyncStatus returns a status snapshot suitable for determining whether
	// it is safe to perform automated sync operations.
	RepoSyncStatus(ctx context.Context, dir string) (RepoSyncStatus, error)

	// ListRemoteRepos returns the remotes configured in the repository.
	ListRemoteRepos(ctx context.Context, dir string) ([]RemoteRepo, error)
	// AddRemoteRepo adds a new remote to the repository.
	AddRemoteRepo(ctx context.Context, dir string, remoteName string, remoteURL string) error
	// DeleteRemoteRepo deletes a configured remote from the repository.
	DeleteRemoteRepo(ctx context.Context, dir string, remoteName string) error
	// FetchRemoteRepo fetches updates from a configured remote.
	FetchRemoteRepo(ctx context.Context, dir string, remoteName string) error
	// Merge merges the named branch from the remote into the current
	// branch. If branch is unspecified, it merges from the branch's configured
	// upstream. Similar when both remote and branch are unspecified.
	//
	// Implementations should avoid interactive prompting.
	//
	// If branch is empty, the implementation should merge from the default
	// upstream for the current branch (when available).
	Merge(ctx context.Context, dir string, remoteName string,
		branch string) error

	// Push pushes the current branch to the named remote/branch.
	//
	// If branch is empty, it pushes to the current branch's configured upstream
	// (when available). Similarly when both remoteName and branch are empty.
	//
	// Implementations should avoid interactive prompting.
	Push(ctx context.Context, dir string, remoteName string, branch string) error

	// ListBranches lists branches.
	//
	// When remoteName is empty, only local branches are returned.
	// When remoteName is non-empty, only branches in that remote are returned.
	ListBranches(ctx context.Context, dir string, remoteName string) ([]string, error)

	DiffTool(ctx context.Context, dir string, spec DiffSpec) error
	Commit(ctx context.Context, dir string, opts CommitOptions) (*UntrackedFiles, error)
}
