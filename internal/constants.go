/* Copyright © 2023-2025 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package internal

const MaxDepth = 3

// The following variables are intended to be set at build-time via:
//
//	go build -ldflags "-X github.com/mikeb26/gptcli/internal.CliToolName=..."
//
// (typically wired through the top-level Makefile).
//
// They must be vars (not consts) for -ldflags=-X to work.
var (
	CliToolName         = "gptcli"
	CliSandboxUsername  = "aiagent"
	CliSandboxGroupname = "gptcli-share"
	// CliLibexecDir is the base directory where gptcli's privileged helpers
	// (such as the run-as-<sandbox> wrapper) are installed by packaging.
	//
	// It is a var (not const) so tests can override it.
	CliLibexecDir = "/usr/libexec"
)
