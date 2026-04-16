/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/types"
)

// NuxPrefs stores whether various onboarding / help prompts have been
// dismissed.
//
// NOTE: The Seen* names are historical: a Seen* flag being true means the user
// has chosen "Don't show again".
//
// Defaults are false so new users see helpful prompts until they explicitly
// dismiss them.
type NuxPrefs struct {
	SeenWelcome             bool `json:"seen_welcome"`
	SeenThreadMenuEmpty     bool `json:"seen_thread_menu_empty"`
	SeenThreadMenuHints     bool `json:"seen_thread_menu_hints"`
	SeenThreadViewHelp      bool `json:"seen_thread_view_help"`
	SeenThreadDetachHelp    bool `json:"seen_thread_detach_help"`
	SeenWorkspaceIntro      bool `json:"seen_workspace_intro"`
	SeenApprovalsIntro      bool `json:"seen_approvals_intro"`
	SeenRunCommandIntro     bool `json:"seen_run_command_intro"`
	SeenWorkspacePushIntro  bool `json:"seen_workspace_push_intro"`
	SeenWorkspaceMergeIntro bool `json:"seen_workspace_merge_intro"`
	SeenWorkspaceResetIntro bool `json:"seen_workspace_reset_intro"`
}

func DefaultNuxPrefs() NuxPrefs {
	// false => not dismissed => show by default
	return NuxPrefs{}
}

// NUX keys used for per-process session suppression.
const (
	nuxKeyWelcome         = "welcome"
	nuxKeyThreadViewHelp  = "thread_view_help"
	nuxKeyThreadDetachHelp = "thread_detach_help"
	nuxKeyMenuEmpty       = "menu_empty"
	nuxKeyMenuHints       = "menu_hints"
	nuxKeyWorkspaceIntro  = "workspace_intro"
	nuxKeyApprovalsIntro  = "approvals_intro"
	nuxKeyRunCommandIntro = "run_command_intro"

	nuxKeyWorkspacePushIntro  = "workspace_push_intro"
	nuxKeyWorkspaceMergeIntro = "workspace_merge_intro"
	nuxKeyWorkspaceResetIntro = "workspace_reset_intro"
)

func (cliCtx *CliContext) nuxWasShownThisSession(key string) bool {
	if cliCtx.nuxSessionShown == nil {
		cliCtx.nuxSessionShown = make(map[string]bool)
	}
	return cliCtx.nuxSessionShown[key]
}

func (cliCtx *CliContext) markNuxShownThisSession(key string) {
	if cliCtx.nuxSessionShown == nil {
		cliCtx.nuxSessionShown = make(map[string]bool)
	}
	cliCtx.nuxSessionShown[key] = true
}

func (cliCtx *CliContext) showNuxConfirmDontShowAgain(prompt string) (dontShowAgain bool, err error) {
	choices := []types.UIOption{
		{Key: "ok", Label: "OK"},
		{Key: "dna", Label: "Don't show again"},
	}
	sel, err := cliCtx.ui.SelectOption(prompt, choices)
	if err != nil {
		return false, err
	}
	return sel.Key == "dna", nil
}

func (cliCtx *CliContext) showNuxConfirmDontShowAgainOrQuit(prompt string) (dontShowAgain bool, quit bool, err error) {
	choices := []types.UIOption{
		{Key: "continue", Label: "Continue"},
		{Key: "dna", Label: "Don't show again"},
		{Key: "quit", Label: "Quit"},
	}
	sel, err := cliCtx.ui.SelectOption(prompt, choices)
	if err != nil {
		return false, false, err
	}
	switch sel.Key {
	case "dna":
		return true, false, nil
	case "quit":
		return false, true, nil
	default:
		return false, false, nil
	}
}

func (cliCtx *CliContext) nuxWelcomeWizard(ctx context.Context, force bool) {
	// Welcome / setup wizard.
	//
	// Goals:
	//   - Keep the first screen minimal (no quickstart/workspace)
	//   - Drive vendor selection, then API key setup
	//   - Once completed successfully, implicitly dismiss Welcome
	//   - If the user later resets SeenWelcome, re-running this wizard is the
	//     intended behavior.
	if err := ensureConfigDirExists(); err != nil {
		_ = cliCtx.ui.Confirm(err.Error())
		return
	}
	if err := ensureThreadGroupsDirExists(); err != nil {
		_ = cliCtx.ui.Confirm(err.Error())
		return
	}

	if !cliCtx.nuxWasShownThisSession(nuxKeyWelcome) {
		cliCtx.markNuxShownThisSession(nuxKeyWelcome)
		prompt := strings.TrimSpace(fmt.Sprintf(`Welcome to %v.

Before you can use %v, you need to configure an LLM vendor/model and API key.`, internal.CliToolName, internal.CliToolName))
		choices := []types.UIOption{{Key: "continue", Label: "Continue"}, {Key: "quit", Label: "Quit"}}
		sel, err := cliCtx.ui.SelectOption(prompt, choices)
		if err != nil {
			return
		}
		if sel.Key == "quit" {
			cliCtx.exitRequested = true
			return
		}
	}

	for {
		// Step 1: choose vendor (and default model).
		changed, err := chooseVendor(cliCtx)
		if err != nil {
			if uiWasCancelled(err) {
				if !force {
					// User opted out; keep Welcome enabled.
					return
				}
				// Setup is required: give an explicit exit path.
				sel, selErr := cliCtx.ui.SelectOption(
					"Setup is required to continue.",
					[]types.UIOption{{Key: "retry", Label: "Choose vendor"}, {Key: "quit", Label: "Quit"}},
				)
				if selErr != nil {
					return
				}
				if sel.Key == "quit" {
					cliCtx.exitRequested = true
					return
				}
				continue
			}
			_ = cliCtx.ui.Confirm(err.Error())
			continue
		}
		if changed {
			vendorInfo := internal.GetVendorInfo(cliCtx.prefs.Vendor)
			cliCtx.prefs.Model = vendorInfo.DefaultModel
			_ = cliCtx.savePrefs()
		}

		// Step 2: configure API key.
		//
		// For force mode, it's required to proceed.
		for {
			ok, err := minConfigSatisfied(cliCtx.prefs.Vendor)
			if err != nil {
				_ = cliCtx.ui.Confirm(err.Error())
				return
			}
			if ok {
				break
			}

			_, err = configureAPIKey(cliCtx)
			if err != nil {
				if uiWasCancelled(err) {
					if !force {
						// User opted out; keep Welcome enabled.
						return
					}
					sel, selErr := cliCtx.ui.SelectOption(
						"An API key is required to continue.",
						[]types.UIOption{{Key: "retry", Label: "Enter API key"}, {Key: "vendor", Label: "Change vendor"}, {Key: "quit", Label: "Quit"}},
					)
					if selErr != nil {
						return
					}
					switch sel.Key {
					case "vendor":
						goto restartWizard
					case "quit":
						cliCtx.exitRequested = true
						return
					default:
						continue
					}
				}
				_ = cliCtx.ui.Confirm(err.Error())
				continue
			}
		}

		// Success: welcome is implicitly dismissed.
		cliCtx.prefs.NUX.SeenWelcome = true
		_ = cliCtx.savePrefs()

		if err := setupDefaultApprovals(); err != nil {
			_ = cliCtx.ui.Confirm(err.Error())
			return
		}
		// Only reload the runtime if we either don't have one, or we're in
		// first-time setup mode.
		if cliCtx.ictx == nil || cliCtx.toggles.needConfig {
			if err := cliCtx.load(ctx); err != nil {
				_ = cliCtx.ui.Confirm(err.Error())
				return
			}
			cliCtx.menu.resetItems()
		} else {
			// Apply any vendor/model/key changes to the live runtime.
			if err := cliCtx.reloadLLMSettingsFromPrefs(ctx); err != nil {
				_ = cliCtx.ui.Confirm(err.Error())
				return
			}
		}

		// After setup, introduce threads if needed.
		cliCtx.nuxMenuEmptyIfNeeded(len(cliCtx.menu.entries))
		return

	restartWizard:
		continue
	}
}

func (cliCtx *CliContext) nuxWelcomeIfNeeded(ctx context.Context) {
	if cliCtx.toggles.needConfig {
		cliCtx.nuxWelcomeWizard(ctx, true)
		return
	}

	if cliCtx.prefs.NUX.SeenWelcome {
		return
	}

	// If the user has re-enabled Welcome (cleared SeenWelcome), re-run the same
	// vendor/key wizard flow. Successful completion implicitly dismisses Welcome.
	cliCtx.nuxWelcomeWizard(ctx, false)
}

func (cliCtx *CliContext) nuxThreadViewHelpIfNeeded() {
	if cliCtx.prefs.NUX.SeenThreadViewHelp {
		return
	}
	if cliCtx.nuxWasShownThisSession(nuxKeyThreadViewHelp) {
		return
	}
	cliCtx.markNuxShownThisSession(nuxKeyThreadViewHelp)

	prompt := strings.TrimSpace(`This is the thread view:

Tips:
- There are two panes: History (top) and Input (bottom)

- When either is focused:
-   Press Ctrl-D to send your prompt
-
- When History is focused:
-   Press 'ESC' to go back to the main menu
-   Press 'i' to switch focus to Input
-   Press 'w' to link/manage a git repository with this thread (optional)
-   Press 'n' to rename the thread

- When Input is focused:
-   Type the prompt you want to send
-   Press 'ESC' to switch focus back to History
`)

	dna, err := cliCtx.showNuxConfirmDontShowAgain(prompt)
	if err != nil {
		return
	}
	if dna {
		cliCtx.prefs.NUX.SeenThreadViewHelp = true
		_ = cliCtx.savePrefs()
	}
}

func (cliCtx *CliContext) nuxThreadDetachHelpIfNeeded() {
	if cliCtx.prefs.NUX.SeenThreadDetachHelp {
		return
	}
	if cliCtx.nuxWasShownThisSession(nuxKeyThreadDetachHelp) {
		return
	}
	cliCtx.markNuxShownThisSession(nuxKeyThreadDetachHelp)

	prompt := strings.TrimSpace(`Thread started.

You can return to the main menu at any time while this thread is working.

- Press 'ESC' from the history pane to leave the thread view
- The thread will keep running in the background
- You can open the thread again later without losing your place
`)

	dna, err := cliCtx.showNuxConfirmDontShowAgain(prompt)
	if err != nil {
		return
	}
	if dna {
		cliCtx.prefs.NUX.SeenThreadDetachHelp = true
		_ = cliCtx.savePrefs()
	}
}

func (cliCtx *CliContext) nuxMenuEmptyIfNeeded(threadCount int) {
	if threadCount != 0 {
		return
	}
	// NUX ordering: show the welcome modal before any menu empty-state hint.
	if !cliCtx.prefs.NUX.SeenWelcome && !cliCtx.nuxWasShownThisSession(nuxKeyWelcome) {
		return
	}
	if cliCtx.prefs.NUX.SeenThreadMenuEmpty {
		return
	}
	if cliCtx.nuxWasShownThisSession(nuxKeyMenuEmpty) {
		return
	}
	cliCtx.markNuxShownThisSession(nuxKeyMenuEmpty)

	prompt := strings.TrimSpace(`Threads are saved conversations.

To get started:
- Press 'n' to create your first thread
- Press Enter to open it

Other:
- Press 'c' for configuration
- Press ESC to quit
`)

	dna, err := cliCtx.showNuxConfirmDontShowAgain(prompt)
	if err != nil {
		return
	}
	if dna {
		cliCtx.prefs.NUX.SeenThreadMenuEmpty = true
		_ = cliCtx.savePrefs()
	}
}

func (cliCtx *CliContext) nuxMenuHintsIfNeeded() {
	// NUX ordering: show the welcome modal before menu hints.
	if !cliCtx.prefs.NUX.SeenWelcome && !cliCtx.nuxWasShownThisSession(nuxKeyWelcome) {
		return
	}
	if cliCtx.prefs.NUX.SeenThreadMenuHints {
		return
	}
	if cliCtx.nuxWasShownThisSession(nuxKeyMenuHints) {
		return
	}
	cliCtx.markNuxShownThisSession(nuxKeyMenuHints)

	prompt := strings.TrimSpace(`You've made your first thread.

Tips:
- Enter opens the selected thread
- '/' searches threads
- 'a' archives a thread
- 'u' unarchives a thread
`)

	dna, err := cliCtx.showNuxConfirmDontShowAgain(prompt)
	if err != nil {
		return
	}
	if dna {
		cliCtx.prefs.NUX.SeenThreadMenuHints = true
		_ = cliCtx.savePrefs()
	}
}

func (cliCtx *CliContext) nuxWorkspaceIntroIfNeeded() (dontShowAgain bool) {
	if cliCtx.prefs.NUX.SeenWorkspaceIntro {
		return false
	}
	if cliCtx.nuxWasShownThisSession(nuxKeyWorkspaceIntro) {
		return false
	}
	cliCtx.markNuxShownThisSession(nuxKeyWorkspaceIntro)

	prompt := strings.TrimSpace(`Workspace intro:

A workspace links this thread to one of your git repositories (Origin).

- Octium creates an independent clone (Sandbox) of your repo to work with
- Your repo's working tree is not modified directly
- You can sync/push/merge/diff changes between your repo and the sandbox when ready
`)

	dna, err := cliCtx.showNuxConfirmDontShowAgain(prompt)
	if err != nil {
		return false
	}
	if dna {
		cliCtx.prefs.NUX.SeenWorkspaceIntro = true
		_ = cliCtx.savePrefs()
		return true
	}
	return false
}

func (cliCtx *CliContext) nuxApprovalsIntroIfNeeded() (dontShowAgain bool) {
	if cliCtx.prefs.NUX.SeenApprovalsIntro {
		return false
	}
	if cliCtx.nuxWasShownThisSession(nuxKeyApprovalsIntro) {
		return false
	}
	cliCtx.markNuxShownThisSession(nuxKeyApprovalsIntro)

	prompt := strings.TrimSpace(`Approvals:

Octium asks permission before it reads/writes files, runs commands, or accesses the network.

- You can approve once, or approve for a target (directory/domain/command) for future use
- Approvals are stored in approvals.json and can be changed later
`)

	dna, err := cliCtx.showNuxConfirmDontShowAgain(prompt)
	if err != nil {
		return false
	}
	if dna {
		cliCtx.prefs.NUX.SeenApprovalsIntro = true
		_ = cliCtx.savePrefs()
		return true
	}
	return false
}

func (cliCtx *CliContext) nuxRunCommandIntroIfNeeded() (dontShowAgain bool) {
	if cliCtx.prefs.NUX.SeenRunCommandIntro {
		return false
	}
	if cliCtx.nuxWasShownThisSession(nuxKeyRunCommandIntro) {
		return false
	}
	cliCtx.markNuxShownThisSession(nuxKeyRunCommandIntro)

	prompt := strings.TrimSpace(`Running commands:

When the agent runs commands, they execute in a restricted sandbox environment.

- Minimal environment
- Working directory may be the thread's workspace sandbox
- Network is proxied and may require additional approvals
`)

	dna, err := cliCtx.showNuxConfirmDontShowAgain(prompt)
	if err != nil {
		return false
	}
	if dna {
		cliCtx.prefs.NUX.SeenRunCommandIntro = true
		_ = cliCtx.savePrefs()
		return true
	}
	return false
}

func (cliCtx *CliContext) nuxWorkspaceOpIntroIfNeeded(opKey string) {
	var alreadyDismissed bool
	var sessionKey string
	var prompt string
	var markDismissed func()

	switch opKey {
	case nuxKeyWorkspacePushIntro:
		alreadyDismissed = cliCtx.prefs.NUX.SeenWorkspacePushIntro
		sessionKey = nuxKeyWorkspacePushIntro
		prompt = strings.TrimSpace(`Workspace: Push

This pushes committed changes from the sandbox clone into a branch in your repo.

Tip: use Diff/Commit first to review what will be pushed.`)
		markDismissed = func() {
			cliCtx.prefs.NUX.SeenWorkspacePushIntro = true
		}
	case nuxKeyWorkspaceMergeIntro:
		alreadyDismissed = cliCtx.prefs.NUX.SeenWorkspaceMergeIntro
		sessionKey = nuxKeyWorkspaceMergeIntro
		prompt = strings.TrimSpace(`Workspace: Merge

This merges committed changes from the sandbox clone into your repo's current branch.

Tip: push to a branch first if you want a review/PR workflow.`)
		markDismissed = func() {
			cliCtx.prefs.NUX.SeenWorkspaceMergeIntro = true
		}
	case nuxKeyWorkspaceResetIntro:
		alreadyDismissed = cliCtx.prefs.NUX.SeenWorkspaceResetIntro
		sessionKey = nuxKeyWorkspaceResetIntro
		prompt = strings.TrimSpace(`Workspace: Reset

Reset discards the sandbox clone (and any unpushed work in it).

You can also reset + change which origin repo this thread is linked to.`)
		markDismissed = func() {
			cliCtx.prefs.NUX.SeenWorkspaceResetIntro = true
		}
	default:
		return
	}

	if alreadyDismissed {
		return
	}
	if cliCtx.nuxWasShownThisSession(sessionKey) {
		return
	}
	cliCtx.markNuxShownThisSession(sessionKey)

	dna, err := cliCtx.showNuxConfirmDontShowAgain(prompt)
	if err != nil {
		return
	}
	if dna {
		markDismissed()
		_ = cliCtx.savePrefs()
	}
}
