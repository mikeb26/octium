/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package workspace

import "fmt"

var (
	ErrScratchDirNotSet               = fmt.Errorf("scratch dir not set")
	ErrScratchDirCreate               = fmt.Errorf("failed to create scratch dir")
	ErrWorkspaceMarshal               = fmt.Errorf("failed to marshal workspace")
	ErrWorkspaceOpenFile              = fmt.Errorf("failed to open workspace file")
	ErrWorkspaceWriteFile             = fmt.Errorf("failed to write workspace file")
	ErrWorkspaceSyncFile              = fmt.Errorf("failed to sync workspace file")
	ErrWorkspaceCloseFile             = fmt.Errorf("failed to close workspace file")
	ErrWorkspacePersist               = fmt.Errorf("failed to persist workspace")
	ErrWorkspaceReadFile              = fmt.Errorf("failed to read workspace file")
	ErrWorkspaceParseFile             = fmt.Errorf("failed to parse workspace file")
	ErrWorkspaceScratchMismatch       = fmt.Errorf("workspace scratch dir mismatch")
	ErrWorkspaceSandboxSetOriginEmpty = fmt.Errorf("sandbox is set but origin is empty")
	ErrOriginRepoNotSet               = fmt.Errorf("origin repo dir not set")
	ErrOriginRepoInvalid              = fmt.Errorf("invalid origin repo")
	ErrWorkspaceOriginAlreadySet      = fmt.Errorf("workspace already has an origin set")
	ErrWorkspaceSandboxAlreadySet     = fmt.Errorf("workspace already has a sandbox set")
	ErrWorkspaceNoOriginSet           = fmt.Errorf("workspace does not have an origin set")
	ErrWorkspaceNoSandboxSet          = fmt.Errorf("workspace does not have a sandbox set")
	ErrSandboxNoUpstream              = fmt.Errorf("sandbox has no upstream configured")
	ErrSandboxDirty                   = fmt.Errorf("sandbox has uncommitted changes")
	ErrOriginDirty                    = fmt.Errorf("origin has uncommitted changes")
	ErrSandboxInProgress              = fmt.Errorf("sandbox is in an in-progress operation state")
	ErrOriginInProgress               = fmt.Errorf("origin is in an in-progress operation state")
	ErrRepoDoesNotExist               = fmt.Errorf("repo does not exist")
	ErrRepoNotDirectory               = fmt.Errorf("repo is not a directory")
	ErrRepoNotScmRepository           = fmt.Errorf("repo is not an scm repository")
	ErrSandboxNoOriginRemote          = fmt.Errorf("sandbox has no 'origin' remote")
	ErrSandboxOriginMismatch          = fmt.Errorf("sandbox's upstream does not point to originRepo")
	ErrResolveSrcRepoDir              = fmt.Errorf("failed to resolve src repo dir")
	ErrParseOriginURL                 = fmt.Errorf("failed to parse origin url")
	ErrOriginRemoteNotLocalPath       = fmt.Errorf("origin remote url is not a local path")
	ErrResolveOriginPath              = fmt.Errorf("failed to resolve origin path")
	ErrResolveOriginDir               = fmt.Errorf("failed to resolve origin dir")
	ErrSandboxDirAlreadyExists        = fmt.Errorf("sandbox directory already exists")
	ErrSandboxPathNotDirectory        = fmt.Errorf("sandbox path already exists and is not a directory")
	ErrSandboxStatDir                 = fmt.Errorf("failed to stat sandbox dir")
	ErrSandboxRepoInvalid             = fmt.Errorf("invalid sandbox repo")
	ErrResolvePath                    = fmt.Errorf("failed to resolve path")
)
