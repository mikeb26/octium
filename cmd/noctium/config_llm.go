/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/types"
)

func configLLM(cliCtx *CliContext) (needReload bool, err error) {
	for {
		keyConfigured, err := apiKeyConfigured(cliCtx.prefs.Vendor)
		if err != nil {
			return false, err
		}
		apiKeyStatus := "not set"
		if keyConfigured {
			apiKeyStatus = "set"
		}

		choices := []types.UIOption{
			{Key: "vendor", Label: fmt.Sprintf("Vendor [%s]", cliCtx.prefs.Vendor)},
			{Key: "model", Label: fmt.Sprintf("Model [%s]", cliCtx.prefs.Model)},
			{Key: "key", Label: fmt.Sprintf("API key [%s]", apiKeyStatus)},
			{Key: "back", Label: "Back"},
		}
		sel, err := cliCtx.ui.SelectOption("LLM config:", choices)
		if err != nil {
			return needReload, err
		}

		switch sel.Key {
		case "vendor":
			changed, err := chooseVendor(cliCtx)
			if err != nil {
				return needReload, err
			}
			if changed {
				needReload = true
				// Vendor change implies the model list changes; reset to default.
				vendorInfo := internal.GetVendorInfo(cliCtx.prefs.Vendor)
				cliCtx.prefs.Model = vendorInfo.DefaultModel
				if err := cliCtx.savePrefs(); err != nil {
					return needReload, err
				}
			}
		case "model":
			changed, err := chooseModel(cliCtx)
			if err != nil {
				return needReload, err
			}
			if changed {
				needReload = true
			}
		case "key":
			changed, err := configureAPIKey(cliCtx)
			if err != nil {
				return needReload, err
			}
			if changed {
				needReload = true
			}
		case "back":
			return needReload, nil
		default:
			_ = cliCtx.ui.Confirm("Invalid selection")
		}
	}
}

func chooseVendor(cliCtx *CliContext) (changed bool, err error) {
	vendorKeys := internal.GetVendors()
	sort.Strings(vendorKeys)
	choices := make([]types.UIOption, 0, len(vendorKeys))
	for _, v := range vendorKeys {
		fullName := internal.GetVendorInfo(v).FullName
		label := fullName
		if v == cliCtx.prefs.Vendor {
			label = fullName + "*"
		}
		choices = append(choices, types.UIOption{Key: v, Label: label})
	}

	selection, err := cliCtx.ui.SelectOption("Choose an LLM vendor:", choices)
	if err != nil {
		return false, err
	}
	vendor := strings.ToLower(strings.TrimSpace(selection.Key))
	if vendor == "" {
		return false, fmt.Errorf("%w: empty vendor", ErrUnsupportedVendor)
	}
	if vendor == cliCtx.prefs.Vendor {
		return false, nil
	}

	cliCtx.prefs.Vendor = vendor
	if err := cliCtx.savePrefs(); err != nil {
		return false, err
	}
	return true, nil
}

func chooseModel(cliCtx *CliContext) (changed bool, err error) {
	vendor := strings.TrimSpace(cliCtx.prefs.Vendor)
	vendorInfo := internal.GetVendorInfo(vendor)
	models := vendorInfo.SupportedModels
	choices := make([]types.UIOption, 0, len(models))
	for _, m := range models {
		label := m
		if m == cliCtx.prefs.Model {
			label = m + "*"
		}
		choices = append(choices, types.UIOption{Key: m, Label: label})
	}
	selection, err := cliCtx.ui.SelectOption(
		fmt.Sprintf("Choose a %v model:", vendorInfo.FullName), choices)
	if err != nil {
		return false, err
	}
	model := strings.ToLower(strings.TrimSpace(selection.Key))
	if model == "" {
		return false, fmt.Errorf("%w: empty model", ErrUnsupportedModel)
	}
	if model == cliCtx.prefs.Model {
		return false, nil
	}
	cliCtx.prefs.Model = model
	if err := cliCtx.savePrefs(); err != nil {
		return false, err
	}
	return true, nil
}

func configureAPIKey(cliCtx *CliContext) (changed bool, err error) {
	configDir, err := getConfigDir()
	if err != nil {
		return false, err
	}
	vendor := strings.TrimSpace(cliCtx.prefs.Vendor)
	vendorInfo := internal.GetVendorInfo(vendor)
	keyPath := path.Join(configDir, fmt.Sprintf(KeyFileFmt, vendor))
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("%w (%v) %v: %w", ErrCouldNotOpenAPIKeyFile, vendor, keyPath, err)
	}
	existingKey := strings.TrimSpace(string(keyBytes))

	if existingKey != "" {
		choices := []types.UIOption{
			{Key: "keep", Label: "Keep existing key"},
			{Key: "replace", Label: "Replace key"},
			{Key: "back", Label: "Back"},
		}
		sel, err := cliCtx.ui.SelectOption(
			fmt.Sprintf("%v API key is already configured.", vendorInfo.FullName),
			choices,
		)
		if err != nil {
			return false, err
		}
		switch sel.Key {
		case "keep", "back":
			return false, nil
		case "replace":
			// continue below
		default:
			return false, fmt.Errorf("unknown selection: %v", sel.Key)
		}
	}

	keyPrompt := fmt.Sprintf(
		"Please visit %v to obtain an API key.\nEnter your %v API key (ESC to cancel):",
		vendorInfo.ApiKeyUrl,
		vendorInfo.FullName,
	)
	key, err := cliCtx.ui.Get(keyPrompt)
	if err != nil {
		return false, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false, nil
	}
	err = os.WriteFile(keyPath, []byte(key), 0600)
	if err != nil {
		return false, fmt.Errorf("%w (%v) %v: %w", ErrCouldNotWriteAPIKeyFile, vendor, keyPath, err)
	}
	return true, nil
}

func apiKeyConfigured(vendor string) (bool, error) {
	keyPath, err := getKeyPath(vendor)
	if err != nil {
		return false, err
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(keyBytes)) != "", nil
}

func getKeyPath(vendor string) (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, fmt.Sprintf(KeyFileFmt, vendor)), nil
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
