/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"

	gc "github.com/rthornton128/goncurses"

	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/am"
	"github.com/mikeb26/octium/internal/fsatomic"
	"github.com/mikeb26/octium/internal/fsatomic/local"
	"github.com/mikeb26/octium/internal/httpproxy"
	"github.com/mikeb26/octium/internal/llmclient"
	"github.com/mikeb26/octium/internal/scm"
	"github.com/mikeb26/octium/internal/scm/git"
	"github.com/mikeb26/octium/internal/threads"
	"github.com/mikeb26/octium/internal/types"
	"github.com/mikeb26/octium/internal/types/aiclient"
	"github.com/mikeb26/octium/internal/ui"
)

const (
	KeyFileFmt             = ".%v.key"
	PrefsFile              = "prefs.json"
	ApprovePolicyFile      = "approvals.json"
	ThreadsDirOld          = "threads"
	ArchiveDirOld          = "archive_threads"
	ThreadGroupsDir        = "thread_groups"
	LogsDir                = "logs"
	AuditLogFile           = "audit.log"
	MainThreadGroupName    = "main"
	ArchiveThreadGroupName = "archive"
)

type Prefs struct {
	SummarizePrior bool   `json:"summarize_prior"`
	Vendor         string `json:"vendor"`
	Model          string `json:"model"`
	Reasoning      string `json:"reasoning_effort"`
	RunCmdApproval bool   `json:"run_cmd_approval"`
	EnableAuditLog bool   `json:"enable_audit_log"`
	WrapMode       string `json:"wrap_mode"`
}

type Toggles struct {
	summary    bool
	useColors  bool
	needConfig bool
	wrapMode   ui.WrapMode
}

type CliContext struct {
	ictx *types.InternalContext
	Afs  fsatomic.AtomicFS

	ui          *ui.NcursesUI
	rootWin     *gc.Window
	menu        *threadMenuUI
	threadViews map[string]*threadViewUI

	prefs   Prefs
	toggles Toggles

	// llmClient for non-persistent, fast 1-shot low latency completions.
	// for persistent, slower, long dialogues see internal/threads/thread.llmClient
	llmClient      aiclient.AIClient
	threadGroupSet *threads.ThreadGroupSet
	curThreadGroup string

	scmClient scm.Client
}

func NewCliContext(ctx context.Context) (*CliContext, error) {
	var err error
	var rootWinLocal *gc.Window
	rootWinLocal, err = gcInit()
	if err != nil {
		return nil, err
	}

	vendor := internal.DefaultVendor
	model := internal.GetVendorInfo(vendor).DefaultModel
	cliCtx := &CliContext{
		ui:      ui.NewNcursesUI(rootWinLocal),
		rootWin: rootWinLocal,
		toggles: Toggles{
			summary:    false,
			needConfig: true,
			useColors:  false,
		},
		prefs: Prefs{
			SummarizePrior: false,
			Vendor:         internal.DefaultVendor,
			Model:          model,
			Reasoning:      types.ReasoningEffortMedium.String(),
			RunCmdApproval: false,
			EnableAuditLog: true,
		},
		threadGroupSet: nil,
		threadViews:    make(map[string]*threadViewUI),
		curThreadGroup: MainThreadGroupName,
		scmClient:      git.NewClient(),
	}
	cliCtx.menu = newThreadMenuUI(cliCtx)

	threadGroupsDirLocal, err := getThreadGroupsDir()
	if err != nil {
		threadGroupsDirLocal = "/tmp"
	}

	cliCtx.Afs = local.New()
	cliCtx.threadGroupSet = threads.NewThreadGroupSet(threadGroupsDirLocal,
		[]string{MainThreadGroupName, ArchiveThreadGroupName}, cliCtx.Afs, cliCtx.scmClient)

	return cliCtx, nil
}

func (cliCtx *CliContext) load(ctx context.Context) error {

	cliCtx.toggles.needConfig = true
	err := cliCtx.loadPrefs()
	if err != nil {
		return err
	}
	keyText, err := loadKey(cliCtx.prefs.Vendor)
	if err != nil {
		return err
	}

	if cliCtx.prefs.EnableAuditLog {
		auditLogsDir, err := getLogsDir()
		if err != nil {
			return err
		}
		err = os.MkdirAll(auditLogsDir, 0700)
		if err != nil {
			return fmt.Errorf("%w %v: %w", ErrCouldNotCreateLogsDir, auditLogsDir, err)
		}
	}

	auditLogPath := ""
	if cliCtx.prefs.EnableAuditLog {
		auditLogPath, err = getAuditLogPath()
		if err != nil {
			return err
		}
	}

	policyPath, err := getApprovePolicyPath()
	if err != nil {
		return err
	}
	policyStore, err := am.NewJSONApprovalPolicyStore(policyPath)
	if err != nil {
		return err
	}

	approver := ui.NewUIApprover(cliCtx.ui)
	cliCtx.ictx = types.NewIctx(cliCtx.prefs.Vendor, cliCtx.prefs.Model,
		keyText, auditLogPath, approver, policyStore,
		httpproxy.New(policyStore), cliCtx.Afs)
	cliCtx.ictx.ASettings.RunCmdNeedsApproval = cliCtx.prefs.RunCmdApproval
	cliCtx.ictx.LlmSettings.ReasoningEffort = types.ReasoningEffort(cliCtx.prefs.Reasoning)

	// Apply UI preferences that may not have been available during initial UI
	// initialization.
	var wrapMode ui.WrapMode
	cliCtx.toggles.wrapMode = wrapMode.FromString(cliCtx.prefs.WrapMode)

	err = cliCtx.threadGroupSet.Load(ctx)
	if err != nil {
		return err
	}
	err = cliCtx.ictx.HttpProxy.ListenAndServe()
	if err != nil {
		return fmt.Errorf("%v: Failed to start http proxy: %w\n", internal.CliToolName, err)
	}
	// cliCtx.llmClient is used for internal quick 1-shot purposes; so
	// intentionally set low here and ignore prefs.Reasoning
	cliCtx.llmClient = llmclient.NewEINOClient(ctx, cliCtx.ictx, approver, 0)
	cliCtx.llmClient.SetReasoning(types.ReasoningEffortLow)
	cliCtx.toggles.needConfig = false

	return nil
}

func main() {
	ctx := context.Background()
	cliCtx, err := NewCliContext(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v: Failed to initialize UI: %v\n", internal.CliToolName,
			err)
		os.Exit(1)
	}

	err = cliCtx.migrateOldThreadGroupFomatIfNeeded()
	if err != nil {
		gcExit()
		fmt.Fprintf(os.Stderr, "%v: Failed to migrate existing threads to new format: %v\n", internal.CliToolName, err)
		os.Exit(1)
	}
	err = cliCtx.migrateOldConfigDirIfNeeded()
	if err != nil {
		gcExit()
		fmt.Fprintf(os.Stderr, "%v: Failed to migrate old config dir to new: %v\n", internal.CliToolName, err)
		os.Exit(1)
	}

	err = cliCtx.load(ctx)
	if err != nil && !cliCtx.toggles.needConfig {
		gcExit()
		fmt.Fprintf(os.Stderr, "%v: Failed to load: %v\n", internal.CliToolName, err)
		os.Exit(1)
	}

	err = showMenu(ctx, cliCtx)
	gcExit()

	if err == io.EOF {
		fmt.Fprintf(os.Stderr, "Upgrade complete. Please restart.")
		os.Exit(1)
	}
}
