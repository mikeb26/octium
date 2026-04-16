/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/am"
	"github.com/mikeb26/octium/internal/llmclient"
	"github.com/mikeb26/octium/internal/types"
	"github.com/mikeb26/octium/internal/ui"
)

// First-time setup default: allow file read/write operations within
// the user's sandbox repo directory tree.
func setupDefaultApprovals() error {
	policyPath, err := getApprovePolicyPath()
	if err != nil {
		return err
	}
	// Only apply this default on first-time setup, when there is no
	// approvals.json yet.
	if _, err := os.Stat(policyPath); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	usr, err := user.Current()
	if err != nil {
		return err
	}

	domain := filepath.Join(internal.CliSandboxRepoHome(usr.Username), usr.Username)
	policyID := am.ApprovalPolicyID(am.ApprovalSubsysTools, am.ApprovalGroupFileIO,
		am.ApprovalTargetDir, domain)

	store, err := am.NewJSONApprovalPolicyStore(policyPath)
	if err != nil {
		return err
	}
	store.Save(policyID, []am.ApprovalAction{am.ApprovalActionRead, am.ApprovalActionWrite})
	return nil
}

func (octiumCtx *CliContext) loadPrefs() error {
	const defaultWrapMode = ui.WrapModeWord
	const defaultThreadViewFocus = threadViewDefaultFocusInput
	vendor := internal.DefaultVendor
	vendorInfo := internal.GetVendorInfo(vendor)
	model := vendorInfo.DefaultModel
	// Establish defaults so newly added prefs fields take the intended defaults
	// even when loading older prefs.json files that don't include them.
	octiumCtx.prefs = Prefs{
		SummarizePrior:         false,
		Vendor:                 vendor,
		Model:                  model,
		Reasoning:              types.ReasoningEffortMedium.String(),
		ShowReasoningInInput:   false,
		RunCmdApproval:         false,
		EnableAuditLog:         true,
		WrapMode:               defaultWrapMode.String(),
		ThreadViewDefaultFocus: defaultThreadViewFocus,
		AutoPromptWorkspaceSetupOnThreadEnter: false,
		NUX:                    DefaultNuxPrefs(),
	}

	filePath, err := getPrefsPath()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToGetPrefsPath, err)
	}
	prefsFileContent, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// First-time run: prefs haven't been created yet. Keep defaults.
			return nil
		}
		return fmt.Errorf("%w: %w", ErrFailedToReadPrefs, err)
	}
	err = json.Unmarshal(prefsFileContent, &octiumCtx.prefs)
	if err != nil {
		return err
	}
	// Backwards compatibility: if older prefs.json doesn't have nux fields,
	// DefaultNuxPrefs() ensures we still get the intended defaults.
	// If the user explicitly set nux fields, the JSON unmarshal above already
	// applied them.
	octiumCtx.toggles.summary = octiumCtx.prefs.SummarizePrior
	if strings.TrimSpace(octiumCtx.prefs.WrapMode) == "" {
		octiumCtx.prefs.WrapMode = defaultWrapMode.String()
	}
	if strings.TrimSpace(octiumCtx.prefs.ThreadViewDefaultFocus) == "" {
		octiumCtx.prefs.ThreadViewDefaultFocus = defaultThreadViewFocus
	}
	if strings.TrimSpace(octiumCtx.prefs.Reasoning) == "" {
		octiumCtx.prefs.Reasoning = types.ReasoningEffortMedium.String()
	}

	octiumCtx.prefs.ThreadViewDefaultFocus = strings.ToLower(strings.TrimSpace(octiumCtx.prefs.ThreadViewDefaultFocus))
	switch octiumCtx.prefs.ThreadViewDefaultFocus {
	case threadViewDefaultFocusInput, threadViewDefaultFocusHistory:
		// ok
	default:
		return fmt.Errorf("%w: %q", ErrUnknownThreadViewDefaultFocus, octiumCtx.prefs.ThreadViewDefaultFocus)
	}

	var wrapMode ui.WrapMode
	octiumCtx.toggles.wrapMode = wrapMode.FromString(octiumCtx.prefs.WrapMode)
	if octiumCtx.prefs.Vendor == "" {
		octiumCtx.prefs.Vendor = vendor
	}
	if octiumCtx.prefs.Model == "" {
		octiumCtx.prefs.Model = model
	}

	octiumCtx.prefs.Reasoning = strings.ToLower(strings.TrimSpace(octiumCtx.prefs.Reasoning))
	switch octiumCtx.prefs.Reasoning {
	case types.ReasoningEffortLow.String(),
		types.ReasoningEffortMedium.String(),
		types.ReasoningEffortHigh.String():
		// ok
	default:
		return fmt.Errorf("%w: %q", ErrUnknownReasoningEffort, octiumCtx.prefs.Reasoning)
	}
	if octiumCtx.ictx != nil {
		octiumCtx.ictx.LlmSettings.ReasoningEffort = types.ReasoningEffort(octiumCtx.prefs.Reasoning)
	}
	return nil
}

func (octiumCtx *CliContext) savePrefs() error {
	prefsFileContent, err := json.Marshal(octiumCtx.prefs)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToMarshalPrefs, err)
	}

	filePath, err := getPrefsPath()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToGetPrefsPath, err)
	}
	err = os.WriteFile(filePath, prefsFileContent, 0600)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToSavePrefs, err)
	}

	return nil
}

func configMain(ctx context.Context, octiumCtx *CliContext) error {
	if err := ensureConfigDirExists(); err != nil {
		return err
	}
	if err := ensureThreadGroupsDirExists(); err != nil {
		return err
	}

	needReload := false
	for {
		choices := []types.UIOption{
			{Key: "llm", Label: fmt.Sprintf("LLM config")},
			{Key: "prefs", Label: fmt.Sprintf("Preferences")},
			{Key: "sec", Label: fmt.Sprintf("Security")},
			{Key: "back", Label: "Back"},
		}

		sel, err := octiumCtx.ui.SelectOption("Configuration:", choices)
		if err != nil {
			if uiWasCancelled(err) {
				break
			}
			return err
		}

		switch sel.Key {
		case "llm":
			didReload, err := configLLM(octiumCtx)
			if err != nil {
				if uiWasCancelled(err) {
					continue
				}
				_ = octiumCtx.ui.Confirm(err.Error())
				continue
			}
			needReload = needReload || didReload
		case "prefs":
			if err := configPreferences(octiumCtx); err != nil {
				if uiWasCancelled(err) {
					continue
				}
				_ = octiumCtx.ui.Confirm(err.Error())
			}
		case "sec":
			didReload, err := configSecurity(octiumCtx)
			if err != nil {
				if uiWasCancelled(err) {
					continue
				}
				_ = octiumCtx.ui.Confirm(err.Error())
				continue
			}
			needReload = needReload || didReload
		case "back":
			goto done
		default:
			_ = octiumCtx.ui.Confirm(fmt.Sprintf("unknown config option: %v", sel.Key))
		}
	}

done:
	if err := setupDefaultApprovals(); err != nil {
		return err
	}

	if !needReload {
		return nil
	}
	if ok, err := minConfigSatisfied(octiumCtx.prefs.Vendor); err != nil {
		return err
	} else if !ok {
		// Not configured enough to load the full runtime (no API key).
		return nil
	}

	// If we already have a live runtime (ictx/proxy/threads), prefer an in-place
	// update of LLM settings rather than a full reload.
	//
	// Full reloads can fail when there are non-idle threads (running/blocked), and
	// can also restart long-lived components like the HTTP proxy.
	if octiumCtx.ictx == nil || octiumCtx.toggles.needConfig {
		return octiumCtx.load(ctx)
	}
	return octiumCtx.reloadLLMSettingsFromPrefs(ctx)
}

// reloadLLMSettingsFromPrefs applies the current prefs.json LLM-related settings
// (vendor/model/key/audit-log) to the live runtime without reloading threads
// from disk or restarting long-lived services.
//
// It recreates the CLI's fast 1-shot llmClient and clears per-thread cached
// llmclients so the next invocation in any thread uses the updated vendor/model.
func (cliCtx *CliContext) reloadLLMSettingsFromPrefs(ctx context.Context) error {
	if cliCtx.ictx == nil {
		return nil
	}

	keyText, err := loadKey(cliCtx.prefs.Vendor)
	if err != nil {
		return err
	}

	auditLogPath := ""
	if cliCtx.prefs.EnableAuditLog {
		auditLogsDir, err := getLogsDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(auditLogsDir, 0700); err != nil {
			return fmt.Errorf("%w %v: %w", ErrCouldNotCreateLogsDir, auditLogsDir, err)
		}
		auditLogPath, err = getAuditLogPath()
		if err != nil {
			return err
		}
	}

	cliCtx.ictx.LlmSettings.Vendor = cliCtx.prefs.Vendor
	cliCtx.ictx.LlmSettings.Model = cliCtx.prefs.Model
	cliCtx.ictx.LlmSettings.ApiKey = keyText
	cliCtx.ictx.LlmSettings.AuditLogPath = auditLogPath
	cliCtx.ictx.LlmSettings.ReasoningEffort = types.ReasoningEffort(cliCtx.prefs.Reasoning)

	// Rebuild the non-persistent quick client so 1-shot flows pick up the new
	// vendor/model immediately.
	approver := cliCtx.ictx.ASettings.BaseApprover
	cliCtx.llmClient = llmclient.NewEINOClient(ctx, cliCtx.ictx, approver, 0)
	cliCtx.llmClient.SetReasoning(types.ReasoningEffortLow)

	// Drop any cached per-thread clients so subsequent runs use the updated
	// vendor/model/key/audit settings.
	if cliCtx.threadGroupSet != nil {
		cliCtx.threadGroupSet.ResetLLMClients()
	}

	return nil
}

func configPreferences(cliCtx *CliContext) error {
	for {
		choices := []types.UIOption{
			{Key: "sum", Label: fmt.Sprintf("Summarize dialogue when continuing threads [%s]", onOff(cliCtx.prefs.SummarizePrior))},
			{Key: "reason", Label: fmt.Sprintf("Reasoning effort (low/medium/high) [%s]", cliCtx.prefs.Reasoning)},
			{Key: "reasonInput", Label: fmt.Sprintf("Show reasoning in input pane while running [%s]", onOff(cliCtx.prefs.ShowReasoningInInput))},
			{Key: "wsAuto", Label: fmt.Sprintf("Auto-prompt workspace setup when entering a thread [%s]", onOff(cliCtx.prefs.AutoPromptWorkspaceSetupOnThreadEnter))},
			{Key: "focus", Label: fmt.Sprintf("Thread view default focus (input/history) [%s]", cliCtx.prefs.ThreadViewDefaultFocus)},
			{Key: "wrap", Label: fmt.Sprintf("Text wrapping in frames [%s]", cliCtx.prefs.WrapMode)},
			{Key: "nux", Label: "New user experience (help modals)"},
			{Key: "back", Label: "Back"},
		}
		sel, err := cliCtx.ui.SelectOption("Preferences:", choices)
		if err != nil {
			return err
		}

		switch sel.Key {
		case "sum":
			vendorInfo := internal.GetVendorInfo(cliCtx.prefs.Vendor)
			prompt := fmt.Sprintf(
				"Summarize dialogue when continuing threads? (reduces costs for less precise replies from %v)",
				vendorInfo.FullName,
			)
			trueOpt := types.UIOption{Key: "y", Label: "Yes"}
			falseOpt := types.UIOption{Key: "n", Label: "No"}
			defaultVal := cliCtx.prefs.SummarizePrior
			summarize, err := cliCtx.ui.SelectBool(prompt, trueOpt, falseOpt, &defaultVal)
			if err != nil {
				return err
			}
			cliCtx.prefs.SummarizePrior = summarize
			cliCtx.toggles.summary = summarize
			if err := cliCtx.savePrefs(); err != nil {
				return err
			}
		case "reason":
			choices := []types.UIOption{
				{Key: types.ReasoningEffortLow.String(), Label: "Low"},
				{Key: types.ReasoningEffortMedium.String(), Label: "Medium (default)"},
				{Key: types.ReasoningEffortHigh.String(), Label: "High"},
			}
			sel, err := cliCtx.ui.SelectOption("Reasoning effort:", choices)
			if err != nil {
				if uiWasCancelled(err) {
					continue
				}
				return err
			}
			cliCtx.prefs.Reasoning = sel.Key
			if cliCtx.ictx != nil {
				cliCtx.ictx.LlmSettings.ReasoningEffort = types.ReasoningEffort(sel.Key)
			}
			if cliCtx.llmClient != nil {
				cliCtx.llmClient.SetReasoning(types.ReasoningEffort(sel.Key))
			}
			if err := cliCtx.savePrefs(); err != nil {
				return err
			}
		case "reasonInput":
			prompt := "Show reasoning content in the input pane while a thread is running?\n\nNote: Currently for most models the delay before reasoning text is returned makes this mostly unhelpful, hence the default of No."
			trueOpt := types.UIOption{Key: "y", Label: "Yes"}
			falseOpt := types.UIOption{Key: "n", Label: "No"}
			defaultVal := cliCtx.prefs.ShowReasoningInInput
			show, err := cliCtx.ui.SelectBool(prompt, trueOpt, falseOpt, &defaultVal)
			if err != nil {
				return err
			}
			cliCtx.prefs.ShowReasoningInInput = show
			if err := cliCtx.savePrefs(); err != nil {
				return err
			}
		case "wsAuto":
			prompt := "Auto-prompt to link/configure a workspace when entering a new thread view?\n\nIf enabled, Octium will offer to link the thread to a git repository when you open a thread that does not yet have a workspace configured.\n\nIf you choose 'configure later' during the prompt, Octium will not prompt again for that thread unless you open the workspace menu ('w')."
			trueOpt := types.UIOption{Key: "y", Label: "Yes"}
			falseOpt := types.UIOption{Key: "n", Label: "No"}
			defaultVal := cliCtx.prefs.AutoPromptWorkspaceSetupOnThreadEnter
			auto, err := cliCtx.ui.SelectBool(prompt, trueOpt, falseOpt, &defaultVal)
			if err != nil {
				return err
			}
			cliCtx.prefs.AutoPromptWorkspaceSetupOnThreadEnter = auto
			if err := cliCtx.savePrefs(); err != nil {
				return err
			}
		case "focus":
			choices := []types.UIOption{
				{Key: threadViewDefaultFocusInput, Label: "Input (default)"},
				{Key: threadViewDefaultFocusHistory, Label: "History"},
			}
			sel, err := cliCtx.ui.SelectOption("Thread view default focus:", choices)
			if err != nil {
				if uiWasCancelled(err) {
					continue
				}
				return err
			}
			cliCtx.prefs.ThreadViewDefaultFocus = sel.Key
			if err := cliCtx.savePrefs(); err != nil {
				return err
			}
		case "wrap":
			choices := []types.UIOption{
				{Key: "hard", Label: "Hard wrap (break at any character)"},
				{Key: "word", Label: "Word wrap (break at spaces; hang-indent lists)"},
				{Key: "off", Label: "Off (truncate lines)"},
			}
			sel, err := cliCtx.ui.SelectOption("Text wrapping:", choices)
			if err != nil {
				if uiWasCancelled(err) {
					continue
				}
				return err
			}
			cliCtx.prefs.WrapMode = sel.Key
			var wrapMode ui.WrapMode
			cliCtx.toggles.wrapMode = wrapMode.FromString(sel.Key)
			// Ensure new modals use the updated wrap mode immediately.
			cliCtx.ui.SetTheme(ui.Theme{UseColors: cliCtx.toggles.useColors, SelectedPair: menuColorSelected, WrapMode: cliCtx.toggles.wrapMode})
			if err := cliCtx.savePrefs(); err != nil {
				return err
			}
		case "nux":
			if err := configNUXPreferences(cliCtx); err != nil {
				if uiWasCancelled(err) {
					continue
				}
				return err
			}
		case "back":
			return nil
		default:
			_ = cliCtx.ui.Confirm("Invalid selection")
		}
	}
}

func configNUXPreferences(cliCtx *CliContext) error {
	for {
		choices := []types.UIOption{
			{Key: "off", Label: fmt.Sprintf("Disable all help modals [%s]", onOff(cliCtx.allNuxDismissed()))},
			{Key: "reset", Label: "Re-enable all help modals (reset)"},
			{Key: "back", Label: "Back"},
		}

		sel, err := cliCtx.ui.SelectOption("New user experience:", choices)
		if err != nil {
			return err
		}

		switch sel.Key {
		case "off":
			// Mark every NUX prompt as dismissed.
			cliCtx.dismissAllNux()
			if err := cliCtx.savePrefs(); err != nil {
				return err
			}
		case "reset":
			cliCtx.prefs.NUX = DefaultNuxPrefs()
			if err := cliCtx.savePrefs(); err != nil {
				return err
			}
		case "back":
			return nil
		default:
			_ = cliCtx.ui.Confirm("Invalid selection")
		}
	}
}

func (cliCtx *CliContext) allNuxDismissed() bool {
	n := cliCtx.prefs.NUX
	return n.SeenWelcome &&
		n.SeenThreadMenuEmpty &&
		n.SeenThreadMenuHints &&
		n.SeenThreadViewHelp &&
		n.SeenThreadDetachHelp &&
		n.SeenWorkspaceIntro &&
		n.SeenApprovalsIntro &&
		n.SeenRunCommandIntro &&
		n.SeenWorkspacePushIntro &&
		n.SeenWorkspaceMergeIntro &&
		n.SeenWorkspaceResetIntro
}

func (cliCtx *CliContext) dismissAllNux() {
	cliCtx.prefs.NUX.SeenWelcome = true
	cliCtx.prefs.NUX.SeenThreadMenuEmpty = true
	cliCtx.prefs.NUX.SeenThreadMenuHints = true
	cliCtx.prefs.NUX.SeenThreadViewHelp = true
	cliCtx.prefs.NUX.SeenThreadDetachHelp = true
	cliCtx.prefs.NUX.SeenWorkspaceIntro = true
	cliCtx.prefs.NUX.SeenApprovalsIntro = true
	cliCtx.prefs.NUX.SeenRunCommandIntro = true
	cliCtx.prefs.NUX.SeenWorkspacePushIntro = true
	cliCtx.prefs.NUX.SeenWorkspaceMergeIntro = true
	cliCtx.prefs.NUX.SeenWorkspaceResetIntro = true
}

func configSecurity(cliCtx *CliContext) (needReload bool, err error) {
	for {
		choices := []types.UIOption{
			{Key: "cmd", Label: fmt.Sprintf("Require approvals for running shell commands [%s]", onOff(cliCtx.prefs.RunCmdApproval))},
			{Key: "audit", Label: fmt.Sprintf("Enable audit logging (logs prompts/tool use) [%s]", onOff(cliCtx.prefs.EnableAuditLog))},
			{Key: "back", Label: "Back"},
		}
		sel, err := cliCtx.ui.SelectOption("Security:", choices)
		if err != nil {
			return needReload, err
		}

		switch sel.Key {
		case "cmd":
			prompt := fmt.Sprintf(
				"Require approvals for running shell commands?\n\nNote: it is safe to accept the default (No) since all shell commands are run in a restricted sandbox environment without access to your $HOME or sensitive files on your system. Shell commands can only access files which you later explicitly share in your workspace. See %v if you would like to audit.",
				internal.CliRunAsScriptPath(),
			)
			trueOpt := types.UIOption{Key: "y", Label: "Yes"}
			falseOpt := types.UIOption{Key: "n", Label: "No"}
			defaultVal := cliCtx.prefs.RunCmdApproval
			require, err := cliCtx.ui.SelectBool(prompt, trueOpt, falseOpt, &defaultVal)
			if err != nil {
				return needReload, err
			}
			cliCtx.prefs.RunCmdApproval = require
			if cliCtx.ictx != nil {
				cliCtx.ictx.ASettings.RunCmdNeedsApproval = require
			}
			if err := cliCtx.savePrefs(); err != nil {
				return needReload, err
			}
		case "audit":
			auditLogPath, err := getAuditLogPath()
			if err != nil {
				return needReload, err
			}
			prompt := fmt.Sprintf(
				"Enable audit logging (logs prompts/tool use) to %v?",
				auditLogPath,
			)
			trueOpt := types.UIOption{Key: "y", Label: "Yes"}
			falseOpt := types.UIOption{Key: "n", Label: "No"}
			defaultVal := cliCtx.prefs.EnableAuditLog
			enable, err := cliCtx.ui.SelectBool(prompt, trueOpt, falseOpt, &defaultVal)
			if err != nil {
				return needReload, err
			}
			if enable {
				logsDir, err := getLogsDir()
				if err != nil {
					return needReload, err
				}
				if err := os.MkdirAll(logsDir, 0700); err != nil {
					return needReload, fmt.Errorf("%w %v: %w", ErrCouldNotCreateLogsDir, logsDir, err)
				}
			}

			if enable != cliCtx.prefs.EnableAuditLog {
				needReload = true
			}
			cliCtx.prefs.EnableAuditLog = enable
			if err := cliCtx.savePrefs(); err != nil {
				return needReload, err
			}
		case "back":
			return needReload, nil
		default:
			_ = cliCtx.ui.Confirm("Invalid selection")
		}
	}
}

func ensureConfigDirExists() error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("%w %v: %w", ErrCouldNotCreateConfigDir, configDir, err)
	}
	return nil
}

func ensureThreadGroupsDirExists() error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	threadGroupsPath := path.Join(configDir, ThreadGroupsDir)
	if err := os.MkdirAll(threadGroupsPath, 0700); err != nil {
		return fmt.Errorf("%w %v: %w", ErrCouldNotCreateThreadsDir, threadGroupsPath, err)
	}
	return nil
}

func minConfigSatisfied(vendor string) (bool, error) {
	vendor = strings.TrimSpace(vendor)
	if vendor == "" {
		return false, nil
	}
	return apiKeyConfigured(vendor)
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func uiWasCancelled(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(err.Error()))
	// NcursesUI uses these messages today.
	return strings.Contains(s, "cancel")
}

func getConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCouldNotFindConfigDir, err)
	}

	return filepath.Join(configDir, internal.CliToolName), nil
}

func getConfigDirOld() (string, error) {
	const CliToolNameOld = "gptcli"

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCouldNotFindConfigDir, err)
	}

	return filepath.Join(configDir, CliToolNameOld), nil
}

func getPrefsPath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, PrefsFile), nil
}

func getApprovePolicyPath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, ApprovePolicyFile), nil
}

func getThreadsDirOld() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, ThreadsDirOld), nil
}

func getArchiveDirOld() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, ArchiveDirOld), nil
}

func getThreadGroupsDir() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, ThreadGroupsDir), nil
}

func getLogsDir() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, LogsDir), nil
}

func getAuditLogPath() (string, error) {
	logsDir, err := getLogsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(logsDir, AuditLogFile), nil
}
