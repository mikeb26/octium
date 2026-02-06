/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package git

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mikeb26/gptcli/internal/scm"
)

// ListRemoteRepos lists configured remotes.
//
// It is equivalent to `git remote -v`.
func (c *Client) ListRemoteRepos(ctx context.Context, dir string) ([]scm.RemoteRepo, error) {
	out, _, err := c.runWithTimeout(ctx, nil, buildGitArgs(dir, "remote", "-v")...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}

	remotes := parseGitRemoteV(out)

	// Provide stable ordering.
	sort.Slice(remotes, func(i, j int) bool {
		return remotes[i].Name < remotes[j].Name
	})
	return remotes, nil
}

// AddRemoteRepo adds a new remote to the repository.
//
// It is equivalent to `git remote add <remoteName> <remoteURL>`.
func (c *Client) AddRemoteRepo(ctx context.Context, dir string, remoteName string, remoteURL string) error {
	remoteName = strings.TrimSpace(remoteName)
	if remoteName == "" {
		return ErrRemoteNameRequired
	}
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return ErrRemoteURLRequired
	}

	_, _, err := c.runWithTimeout(ctx, nil, buildGitArgs(dir, "remote", "add", remoteName, remoteURL)...)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	return nil
}

// DeleteRemoteRepo deletes a configured remote from the repository.
//
// It is equivalent to `git remote remove <remoteName>`.
func (c *Client) DeleteRemoteRepo(ctx context.Context, dir string, remoteName string) error {
	remoteName = strings.TrimSpace(remoteName)
	if remoteName == "" {
		return ErrRemoteNameRequired
	}

	_, _, err := c.runWithTimeout(ctx, nil, buildGitArgs(dir, "remote", "remove", remoteName)...)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	return nil
}

// FetchRemoteRepo fetches updates from a configured remote.
func (c *Client) FetchRemoteRepo(ctx context.Context, dir string, remoteName string) error {
	remoteName = strings.TrimSpace(remoteName)
	if remoteName == "" {
		return ErrRemoteNameRequired
	}

	_, _, err := c.runWithTimeout(ctx, nil, buildGitArgs(dir, "fetch", remoteName)...)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	return nil
}

// ListBranches lists local or remote branches.
func (c *Client) ListBranches(ctx context.Context, dir string, remoteName string) ([]string, error) {
	remoteName = strings.TrimSpace(remoteName)

	args := []string{"branch"}
	if remoteName != "" {
		// --remotes shows refs/remotes/*, and we filter to a specific remote.
		args = append(args, "--remotes", "--list", remoteName+"/*")
	} else {
		// Local branches only.
		args = append(args, "--list")
	}
	// --format avoids the leading "*" and extra whitespace.
	args = append(args, "--format=%(refname:short)")

	out, _, err := c.runWithTimeout(ctx, nil, buildGitArgs(dir, args...)...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}

	branches := splitNonEmptyLines(out)
	sort.Strings(branches)
	return branches, nil
}

// RepoSyncStatus returns SCM-agnostic status information for non-interactive
// sync operations.
func (c *Client) RepoSyncStatus(ctx context.Context, dir string) (scm.RepoSyncStatus, error) {
	meta, flags, err := c.readPorcelainMetaFlags(ctx, dir)
	if err != nil {
		return scm.RepoSyncStatus{}, err
	}

	// Determine the currently checked-out branch best-effort.
	//
	// RepoSyncStatus should be resilient even when the repo is in a detached
	// HEAD state.
	localBranch, _ := c.currentBranch(ctx, dir)

	op := c.inProgressOperation(ctx, dir)
	operation := mapOperation(op)

	remote, branch := splitUpstream(meta.upstream)
	return scm.RepoSyncStatus{
		LocalBranch:           localBranch,
		UpstreamRemote:        remote,
		UpstreamBranch:        branch,
		Ahead:                 meta.ahead,
		Behind:                meta.behind,
		HasUncommittedChanges: flags.staged || flags.unstaged || flags.untracked,
		Operation:             operation,
	}, nil
}

// MergeUpstream merges the configured upstream into the current branch.
func (c *Client) Merge(ctx context.Context, dir string, remoteName string,
	branch string) error {

	meta, flags, err := c.readPorcelainMetaFlags(ctx, dir)
	if err != nil {
		return err
	}
	if flags.staged || flags.unstaged || flags.untracked {
		return fmt.Errorf("local changes are present; refusing to merge")
	}
	if mapOperation(c.inProgressOperation(ctx, dir)) != scm.RepoOperationNormal {
		return fmt.Errorf("repository has an in-progress operation; refusing to merge")
	}

	// When a remote name is specified but the branch is not, interpret this as
	// merging the remote branch with the same name as the currently checked out
	// local branch.
	//
	// This supports workflows like merging a workspace sandbox (added as a
	// temporary remote) into the origin repo's current branch.
	if strings.TrimSpace(remoteName) != "" && strings.TrimSpace(branch) == "" {
		b, err := c.currentBranch(ctx, dir)
		if err != nil {
			return err
		}
		branch = b
	}

	target, err := c.resolveMergeTarget(remoteName, branch, meta)
	if err != nil {
		return err
	}

	// Attempt a normal (non-ff) merge. If it conflicts, we must leave the repo
	// in the conflict state.
	_, _, err = c.runWithTimeout(ctx, nil, buildGitArgs(dir, "merge", "--no-edit", target)...)
	if err != nil {
		if isGitExitCode(err, 1) {
			return scm.ErrMergeConflicts
		}
		return fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}

	return nil
}

func (c *Client) currentBranch(ctx context.Context, dir string) (string, error) {
	out, _, err := c.runWithTimeout(ctx, nil, buildGitArgs(dir, "symbolic-ref", "--quiet", "--short", "HEAD")...)
	if err != nil {
		return "", ErrFailedToDetermineBranch
	}
	b := strings.TrimSpace(out)
	if b == "" {
		return "", ErrFailedToDetermineBranch
	}
	return b, nil
}

func splitUpstream(upstream string) (remote string, branch string) {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		return "", ""
	}
	parts := strings.SplitN(upstream, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func mapOperation(op string) scm.RepoOperation {
	switch op {
	case "":
		return scm.RepoOperationNormal
	case "|MERGING":
		return scm.RepoOperationMerging
	case "|REBASE":
		return scm.RepoOperationRebasing
	case "|CHERRY-PICKING":
		return scm.RepoOperationCherryPicking
	case "|REVERTING":
		return scm.RepoOperationReverting
	case "|BISECTING":
		return scm.RepoOperationBisecting
	default:
		return scm.RepoOperationOther
	}
}

func (c *Client) readPorcelainMetaFlags(ctx context.Context, dir string) (porcelainMeta, porcelainFlags, error) {
	statusOut, _, err := c.runWithTimeout(ctx, nil, buildGitArgs(dir, "status", "--porcelain=v2", "--branch")...)
	if err != nil {
		return porcelainMeta{}, porcelainFlags{}, fmt.Errorf("%w: %w", ErrFailedToExecuteGit, err)
	}
	meta, flags := parsePorcelainV2(statusOut)
	return meta, flags, nil
}

func (c *Client) resolveMergeTarget(remoteName string, branch string,
	meta porcelainMeta) (string, error) {

	if remoteName == "" {
		if branch == "" {
			target := strings.TrimSpace(meta.upstream)
			if target == "" {
				return "", fmt.Errorf("no upstream configured for current branch")
			}
			return target, nil
		}

		return branch, nil
	}

	if branch != "" {
		return remoteName + "/" + branch, nil
	}

	// When remoteName is specified, but branch is not, use the configured
	// upstream ref if it matches
	if !strings.HasPrefix(meta.upstream, remoteName+"/") {
		return "", fmt.Errorf("upstream %v does not match remote %v",
			meta.upstream, remoteName)
	}
	return meta.upstream, nil
}

func splitNonEmptyLines(s string) []string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func parseGitRemoteV(out string) []scm.RemoteRepo {
	// Expected lines look like:
	// origin  https://github.com/user/repo (fetch)
	// origin  https://github.com/user/repo (push)
	//
	// But separators may be tabs; URLs may have spaces (rare but possible). We'll
	// parse by identifying (fetch)/(push) suffix first.
	m := make(map[string]*scm.RemoteRepo)

	for _, line := range splitNonEmptyLines(out) {
		// Identify and strip suffix.
		kind := ""
		switch {
		case strings.HasSuffix(line, "(fetch)"):
			kind = "fetch"
			line = strings.TrimSpace(strings.TrimSuffix(line, "(fetch)"))
		case strings.HasSuffix(line, "(push)"):
			kind = "push"
			line = strings.TrimSpace(strings.TrimSuffix(line, "(push)"))
		default:
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		url := strings.TrimSpace(strings.TrimPrefix(line, name))
		if name == "" || url == "" {
			continue
		}

		r, ok := m[name]
		if !ok {
			r = &scm.RemoteRepo{Name: name}
			m[name] = r
		}
		switch kind {
		case "fetch":
			r.FetchURL = url
		case "push":
			r.PushURL = url
		}
	}

	ret := make([]scm.RemoteRepo, 0, len(m))
	for _, v := range m {
		ret = append(ret, *v)
	}
	return ret
}
