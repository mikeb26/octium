/* Copyright © 2025-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/mikeb26/octium/internal"
	"github.com/mikeb26/octium/internal/am"
	"github.com/mikeb26/octium/internal/types"
)

type RunCommandTool struct {
	approver am.Approver
}

type CmdRunReq struct {
	Cmd          string   `json:"cmd" jsonschema:"description=The command to execute (program name or absolute path). Runs in a sandbox (no shell unless you explicitly invoke one)."`
	CmdArgs      []string `json:"cmdargs" jsonschema:"description=A list of arguments to include when executing the command. Arguments are passed exactly (no shell parsing/globbing/expansion)."`
	TruncateSize int      `json:"truncate_size" jsonschema:"description=The maximum size in bytes of each output stream (stdout and stderr) in the response; this limit should be used to prevent context window explosion"`
}

type ContentOutput struct {
	Content               string `json:"content" jsonschema:"description=The (possibly truncated) content"`
	UntruncatedContentLen int    `json:"untrunc_content_len" jsonschema:"description=The length of the untruncated content"`
	WasTruncated          bool   `json:"was_trunc" jsonschema:"description=Set to true when the returned content was truncated; false otherwise"`
}

type CmdRunResp struct {
	Error  string        `json:"error" jsonschema:"description=The error status of the command"`
	Stdout ContentOutput `json:"stdout" jsonschema:"description=The standard output emitted by the command"`
	Stderr ContentOutput `json:"stderr" jsonschema:"description=The standard error emitted by the command"`
}

func (t RunCommandTool) GetOp() types.ToolCallOp {
	return types.RunCommand
}

func (t RunCommandTool) RequiresUserApproval(ictx *types.InternalContext) bool {
	return ictx.ASettings.RunCmdNeedsApproval
}

// BuildApprovalRequest implements ToolWithCustomApproval for
// RunCommandTool to enable richer, cached approvals for OS-level
// command execution. Approvals can be granted for a single
// invocation, for all invocations of a specific command name, or for
// a specific command+argument combination (hashed for brevity).
func (t RunCommandTool) BuildApprovalRequest(ctx context.Context, arg any) (am.ApprovalRequest, error) {
	req, ok := arg.(*CmdRunReq)
	if !ok || req == nil {
		return DefaultApprovalRequest(t, arg), nil
	}

	// Construct stable policy identifiers for the command and the full
	// invocation (command + arguments). The invocation ID uses a hash
	// of the argument vector to keep policy keys manageable while still
	// being specific.
	cmdPolicyID := am.ApprovalPolicyID(am.ApprovalSubsysTools,
		am.ApprovalGroupCommand, am.ApprovalTargetCommand, req.Cmd)

	invocationKey := buildCommandInvocationKey(req.Cmd, req.CmdArgs)
	invocationPolicyID := am.ApprovalPolicyID(am.ApprovalSubsysTools,
		am.ApprovalGroupCommand, am.ApprovalTargetCommandInvocation,
		invocationKey)

	prompt := fmt.Sprintf("%s would like to run OS command: %q with args %q. Allow?",
		internal.CliToolName, req.Cmd, strings.Join(req.CmdArgs, " "))

	choices := []am.ApprovalChoice{
		{
			Key:   "y",
			Label: "Yes, this time only",
			Scope: am.ApprovalScopeOnce,
		},
		{
			Key:      "ci",
			Label:    "Yes, and allow this exact command invocation in the future",
			Scope:    am.ApprovalScopeTarget,
			PolicyID: invocationPolicyID,
			Actions:  []am.ApprovalAction{am.ApprovalActionExecute},
		},
		{
			Key:      "cc",
			Label:    "Yes, and allow any arguments for this command in the future",
			Scope:    am.ApprovalScopeTarget,
			PolicyID: cmdPolicyID,
			Actions:  []am.ApprovalAction{am.ApprovalActionExecute},
		},
		{
			Key:   "n",
			Label: "No",
			Scope: am.ApprovalScopeDeny,
		},
	}

	return am.ApprovalRequest{
		Prompt:          prompt,
		ToolName:        string(t.GetOp()),
		RequiredActions: []am.ApprovalAction{am.ApprovalActionExecute},
		Choices:         choices,
	}, nil
}

// buildCommandInvocationKey creates a concise but stable key for a
// command invocation by hashing its argument vector. This allows us to
// persist approvals for a specific command+args pair without storing
// arbitrarily long or sensitive arguments verbatim in the policy ID.
func buildCommandInvocationKey(cmd string, args []string) string {
	joined := strings.Join(args, "\x00")
	h := sha256.Sum256([]byte(joined))
	return fmt.Sprintf("%s:%s", cmd, hex.EncodeToString(h[:8]))
}

func NewRunCommandTool(approver am.Approver) types.LlmTool {
	t := &RunCommandTool{
		approver: approver,
	}

	return t.Define()
}

func (t RunCommandTool) Define() types.LlmTool {
	const cmdRunDesc = "Execute a single OS-level program directly (no shell by default). Do NOT call shell interpreters such as bash, sh, or zsh, and do NOT use `-lc`, unless the user has explicitly requested shell features (pipes, redirects, &&, ||, etc.).\n\nSandbox/environment notes:\n- Commands run inside a hardened sandbox user account via a privileged wrapper (sudo + systemd-run).\n- The environment is heavily sanitized (env -i) and only a conservative PATH is provided; do not assume your usual shell init files or environment variables.\n- Working directory may be set to the current workspace (if provided by the UI); otherwise it runs under the sandbox user's home.\n- Outbound network from sandboxed commands is forced through a local HTTP proxy and is subject to user approval policies; if a command fails with an HTTP 403/\"forbidden by proxy policy\", request approval via url_retrieve (approval_only=true) and retry.\n- Always capture output via stdout/stderr; interactive prompts may not work as expected."

	ret, err := utils.InferTool(string(t.GetOp()), cmdRunDesc, t.Invoke)
	if err != nil {
		panic(err)
	}

	return ret
}

func (t RunCommandTool) Invoke(ctx context.Context,
	req *CmdRunReq) (*CmdRunResp, error) {

	resp := &CmdRunResp{}

	err := GetUserApproval(ctx, t.approver, t, req)
	if err != nil {
		resp.Error = err.Error()
		return resp, nil
	}

	proxyAddr := ""
	ictx, ok := types.GetIctx(ctx)
	if ok && ictx.HttpProxy != nil {
		proxyAddr = strings.TrimSpace(ictx.HttpProxy.ProxyAddr())
	}
	if proxyAddr == "" {
		resp.Error = ErrProxyNotConfigured.Error()
		return resp, nil
	}

	args := []string{internal.CliRunAsScriptPath(), "--proxy-addr", proxyAddr}
	if pwd, ok := types.GetWorkspacePwd(ctx); ok {
		pwd = strings.TrimSpace(pwd)
		if pwd != "" {
			args = append(args, "--cwd", pwd)
		}
	}
	args = append(args, "--", req.Cmd)
	args = append(args, req.CmdArgs...)

	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.Stdin = os.Stdin
	var stdoutSb strings.Builder
	var stderrSb strings.Builder
	cmd.Stdout = &stdoutSb
	cmd.Stderr = &stderrSb

	err = cmd.Run()
	if err != nil {
		resp.Error = err.Error()
	}
	resp.Stderr = setContent(stderrSb.String(), req.TruncateSize)
	resp.Stdout = setContent(stdoutSb.String(), req.TruncateSize)

	return resp, nil
}

func setContent(c string, maxLen int) ContentOutput {
	ret := ContentOutput{
		Content:               c,
		UntruncatedContentLen: len(c),
	}
	// 0 indicates no truncation
	if maxLen > 0 && ret.UntruncatedContentLen > maxLen {
		ret.Content = ret.Content[:maxLen]
		ret.WasTruncated = true
	}

	return ret
}
