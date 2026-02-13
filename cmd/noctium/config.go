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
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/types"
)

func (octiumCtx *CliContext) loadPrefs() error {
	vendor := internal.DefaultVendor
	vendorInfo := internal.GetVendorInfo(vendor)
	model := vendorInfo.DefaultModel
	// Establish defaults so newly added prefs fields take the intended defaults
	// even when loading older prefs.json files that don't include them.
	octiumCtx.prefs = Prefs{
		SummarizePrior: false,
		Vendor:         vendor,
		Model:          model,
		EnableAuditLog: true,
	}

	filePath, err := getPrefsPath()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToGetPrefsPath, err)
	}
	prefsFileContent, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFailedToReadPrefs, err)
	}
	err = json.Unmarshal(prefsFileContent, &octiumCtx.prefs)
	if err != nil {
		return err
	}
	octiumCtx.toggles.summary = octiumCtx.prefs.SummarizePrior
	if octiumCtx.prefs.Vendor == "" {
		octiumCtx.prefs.Vendor = vendor
	}
	if octiumCtx.prefs.Model == "" {
		octiumCtx.prefs.Model = model
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
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	err = os.MkdirAll(configDir, 0700)
	if err != nil {
		return fmt.Errorf("%w %v: %w", ErrCouldNotCreateConfigDir, configDir, err)
	}

	vendorKeys := internal.GetVendors()
	sort.Strings(vendorKeys)
	choices := make([]types.UIOption, 0, len(vendorKeys))
	for _, v := range vendorKeys {
		fullName := internal.GetVendorInfo(v).FullName
		choices = append(choices, types.UIOption{Key: v, Label: fullName})
	}

	selection, err := octiumCtx.ui.SelectOption("Choose an LLM vendor:", choices)
	if err != nil {
		return err
	}
	vendor := strings.ToLower(strings.TrimSpace(selection.Key))
	if !slices.Contains(vendorKeys, vendor) {
		return fmt.Errorf("%w: %v", ErrUnsupportedVendor, vendor)
	}
	octiumCtx.prefs.Vendor = vendor
	vendorInfo := internal.GetVendorInfo(vendor)

	models := vendorInfo.SupportedModels
	choices = make([]types.UIOption, 0, len(models))
	for _, m := range models {
		choices = append(choices, types.UIOption{Key: m, Label: m})
	}
	selection, err = octiumCtx.ui.SelectOption(
		fmt.Sprintf("Choose an %v model:", vendorInfo.FullName), choices)
	if err != nil {
		return err
	}
	model := strings.ToLower(strings.TrimSpace(selection.Key))
	if !slices.Contains(models, model) {
		return fmt.Errorf("%w: %v", ErrUnsupportedModel, model)
	}
	octiumCtx.prefs.Model = model

	keyPath := path.Join(configDir, fmt.Sprintf(KeyFileFmt, vendor))
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w (%v) %v: %w", ErrCouldNotOpenAPIKeyFile, vendor, keyPath, err)
	}

	existingKey := strings.TrimSpace(string(keyBytes))
	keepKey := false
	if existingKey != "" {
		keepPrompt := fmt.Sprintf(
			"An existing %v API key is already configured. Keep using it?",
			vendorInfo.FullName,
		)
		defaultKeep := true
		trueOpt := types.UIOption{Key: "y", Label: "y"}
		falseOpt := types.UIOption{Key: "n", Label: "n"}
		keepKey, err = octiumCtx.ui.SelectBool(keepPrompt, trueOpt, falseOpt, &defaultKeep)
		if err != nil {
			return err
		}
	}

	if !keepKey {
		keyPrompt := fmt.Sprintf("Please visit %v to obtain an API key.\nEnter your %v API key: ", vendorInfo.ApiKeyUrl, vendorInfo.FullName)
		key, err := octiumCtx.ui.Get(keyPrompt)
		if err != nil {
			return err
		}
		key = strings.TrimSpace(key)
		err = os.WriteFile(keyPath, []byte(key), 0600)
		if err != nil {
			return fmt.Errorf("%w (%v) %v: %w", ErrCouldNotWriteAPIKeyFile, vendor, keyPath, err)
		}
	}
	threadGroupsPath := path.Join(configDir, ThreadGroupsDir)
	err = os.MkdirAll(threadGroupsPath, 0700)
	if err != nil {
		return fmt.Errorf("%w %v: %w", ErrCouldNotCreateThreadsDir,
			threadGroupsPath, err)
	}

	summarizePrompt := fmt.Sprintf(
		"Summarize dialogue when continuing threads? (reduces costs for less precise replies from %v)",
		vendorInfo.FullName,
	)
	defaultSummarize := false
	trueOpt := types.UIOption{Key: "y", Label: "y"}
	falseOpt := types.UIOption{Key: "n", Label: "n"}

	summarize, err := octiumCtx.ui.SelectBool(summarizePrompt, trueOpt, falseOpt, &defaultSummarize)
	if err != nil {
		return err
	}

	octiumCtx.prefs.SummarizePrior = summarize
	octiumCtx.toggles.summary = octiumCtx.prefs.SummarizePrior

	auditLogPath, err := getAuditLogPath()
	if err != nil {
		return err
	}
	auditPrompt := fmt.Sprintf(
		"Enable audit logging (logs prompts/tool use) to %v?",
		auditLogPath,
	)
	defaultAudit := true
	enableAudit, err := octiumCtx.ui.SelectBool(auditPrompt, trueOpt, falseOpt, &defaultAudit)
	if err != nil {
		return err
	}
	octiumCtx.prefs.EnableAuditLog = enableAudit
	if octiumCtx.prefs.EnableAuditLog {
		logsDir, err := getLogsDir()
		if err != nil {
			return err
		}
		err = os.MkdirAll(logsDir, 0700)
		if err != nil {
			return fmt.Errorf("%w %v: %w", ErrCouldNotCreateLogsDir, logsDir, err)
		}
	}

	err = octiumCtx.savePrefs()
	if err != nil {
		return err
	}

	return octiumCtx.load(ctx)
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

func getKeyPath(vendor string) (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, fmt.Sprintf(KeyFileFmt, vendor)), nil
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

func loadKey(vendor string) (string, error) {
	keyPath, err := getKeyPath(vendor)
	if err != nil {
		return "", fmt.Errorf("%w (%v): %w", ErrCouldNotLoadAPIKey, vendor, err)
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%w (%v): run `%v config` to configure", ErrAPIKeyNotConfigured, vendor, internal.CliToolName)
		}
		return "", fmt.Errorf("%w (%v): %w", ErrCouldNotLoadAPIKey, vendor, err)
	}
	return string(data), nil
}
