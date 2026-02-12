/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package internal

import "path/filepath"

const MaxDepth = 3

// The following variables are intended to be set at build-time via:
//
//	go build -ldflags "-X github.com/mikeb26/octium/internal.CliToolName=..."
//
// (typically wired through the top-level Makefile).
//
// They must be vars (not consts) for -ldflags=-X to work.
var (
	CliToolName         = "octium"
	CliSandboxUsername  = "octium"
	CliSandboxGroupname = "octium-users"
	// should match pkg/common/tmpfiles.d/octium-aiagent.conf.in
	CliSandboxShared = "shared"
	// CliLibexecDir is the base directory where octium's privileged helpers
	// (such as the run-as-<sandbox> wrapper) are installed by packaging.
	//
	// It is a var (not const) so tests can override it.
	CliLibexecDir      = filepath.Join(string(filepath.Separator), "usr", "libexec")
	CliSandboxHome     = filepath.Join(string(filepath.Separator), "home", CliSandboxUsername)
	CliSandboxRepoHome = filepath.Join(CliSandboxHome, CliSandboxShared)
)
