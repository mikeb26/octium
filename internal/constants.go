/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package internal

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

const MaxDepth = 3

// The following variables are intended to be set at build-time via:
//
//	go build -ldflags "-X github.com/mikeb26/octium/internal.CliToolName=..."
//
// (typically wired through the top-level Makefile).
//
// They must be vars (not consts) for -ldflags=-X to work.
var (
	CliToolName = "octium"
	// should match pkg/common/tmpfiles.d/octium-aiagent.conf.in
	CliSandboxShared = "shared"
	// CliLibexecDir is the base directory where octium's privileged helpers
	// (such as the run-as-<sandbox> wrapper) are installed by packaging.
	//
	// It is a var (not const) so tests can override it.
	CliLibexecDir = filepath.Join(string(filepath.Separator), "usr", "libexec")

	// CliSandboxRepoHomeBase is an optional override for the per-user sandbox repo
	// home (normally /home/<sandboxuser>/shared).
	//
	// It exists primarily for tests and restricted environments.
	CliSandboxRepoHomeBase = ""
)

// CliEndUsername attempts to determine the username of the interactive end
// user running the octium client.
//
// In normal usage, this is the non-root user invoking the ncurses UI.
func CliEndUsername() string {
	if u, err := user.Current(); err == nil {
		if u.Username != "" {
			return u.Username
		}
	}
	// Best-effort fallback.
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "unknown"
}

// CliSandboxUsername returns the dedicated sandbox user name for a
// given end user.
//
// Example: end user "foo" => sandbox user "octium-foo".
func CliSandboxUsername(endUser string) string {
	return fmt.Sprintf("%s-%s", CliToolName, endUser)
}

// CliSandboxGroupname returns the shared group name used for
// workspace access and wrapper invocation.
//
// Example: end user "foo" => group "octium-foo-shared".
func CliSandboxGroupname(endUser string) string {
	return fmt.Sprintf("%s-shared", CliSandboxUsername(endUser))
}

// CliSandboxHome returns the sandbox user's home directory.
func CliSandboxHome(endUser string) string {
	return filepath.Join(string(filepath.Separator), "home",
		CliSandboxUsername(endUser))
}

// CliSandboxRepoHome returns the base directory under which the
// sandbox workspaces are stored.
func CliSandboxRepoHome(endUser string) string {
	if CliSandboxRepoHomeBase != "" {
		return CliSandboxRepoHomeBase
	}
	return filepath.Join(CliSandboxHome(endUser), CliSandboxShared)
}

// CliRunAsScriptPath returns the absolute path to the privileged wrapper
// script used for executing commands inside the sandbox.
//
// This is computed dynamically so tests can override CliLibexecDir and other
// variables without needing to also update a precomputed path.
//
// see pkg/common/libexec/run-as-aiagent.in
func CliRunAsScriptPath() string {
	endUser := CliEndUsername()
	sbUser := CliSandboxUsername(endUser)
	return filepath.Join(CliLibexecDir, CliToolName, fmt.Sprintf("run-as-%s", sbUser))
}
