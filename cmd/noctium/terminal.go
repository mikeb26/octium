/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mikeb26/octium/internal"
)

func (tvUI *threadViewUI) workspaceTerm(ctx context.Context) error {
	ws := tvUI.thread.Workspace()
	if ws.Sandbox() == "" {
		return nil
	}

	term := strings.TrimSpace(os.Getenv("TERMINAL"))
	if term == "" {
		_ = tvUI.cliCtx.ui.Confirm(ErrTerminalNotConfigured.Error())
		return ErrTerminalNotConfigured
	}
	termArgs := strings.Fields(term)
	if len(termArgs) == 0 {
		_ = tvUI.cliCtx.ui.Confirm(ErrTerminalNotConfigured.Error())
		return ErrTerminalNotConfigured
	}

	// The terminal emulator itself should run as the invoker (so it can access
	// DISPLAY/WAYLAND). We only run the *shell inside the terminal* as the
	// sandbox user via the run-as wrapper.
	shellPath := strings.TrimSpace(os.Getenv("SHELL"))
	if shellPath == "" {
		shellPath = "/bin/bash"
	}

	suspendNCurses()
	defer restoreNCurses()

	// The run-as wrapper enters the sandbox and then executes the given command.
	//
	// Some installations may have an older wrapper that doesn't support newer
	// flags (like --full-network). Detect support so we can still launch the
	// terminal instead of failing with a generic exit status.
	runAsPath := internal.CliRunAsScriptPath()
	runAsArgs := []string{runAsPath}
	if fileContainsString(runAsPath, "--full-network") {
		runAsArgs = append(runAsArgs, "--full-network")
	}
	runAsArgs = append(runAsArgs, "--cwd", ws.Sandbox(), "--")
	// Use a login shell for a nicer interactive experience.
	runAsArgs = append(runAsArgs, shellPath, "-l")

	termExecArgs, err := terminalExecArgs(termArgs)
	if err != nil {
		_ = tvUI.cliCtx.ui.Confirm(err.Error())
		return err
	}
	termExecArgs = append(termExecArgs, "sudo")
	termExecArgs = append(termExecArgs, runAsArgs...)
	cmd := exec.CommandContext(ctx, termArgs[0], termExecArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		_ = tvUI.cliCtx.ui.Confirm(fmt.Sprintf("Failed to launch %v", termArgs[0]))
		return fmt.Errorf("failed to launch %v: %w", termArgs[0], err)
	}

	return nil
}

func fileContainsString(path string, substr string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(b, []byte(substr))
}

func terminalExecArgs(termArgs []string) ([]string, error) {
	if len(termArgs) == 0 {
		return nil, ErrTerminalNotConfigured
	}

	// If TERMINAL already includes an exec marker (e.g. "qterminal -e"), assume
	// the user configured it intentionally and just append the command.
	if terminalArgsHasExecMarker(termArgs) {
		return termArgs[1:], nil
	}

	// Otherwise, choose an execution style based on common terminals.
	switch filepath.Base(termArgs[0]) {
	case "gnome-terminal", "mate-terminal":
		// gnome-terminal style: gnome-terminal -- <cmd> <args...>
		return append(termArgs[1:], "--"), nil
	case "wezterm":
		// wezterm style: wezterm start -- <cmd> <args...>
		return append(termArgs[1:], "start", "--"), nil
	default:
		// xterm/qterminal/konsole/alacritty/kitty style: <term> -e <cmd> <args...>
		return append(termArgs[1:], "-e"), nil
	}
}

func terminalArgsHasExecMarker(termArgs []string) bool {
	for _, a := range termArgs {
		switch a {
		case "-e", "--execute", "-x", "--command", "--":
			return true
		default:
		}
	}
	return false
}
