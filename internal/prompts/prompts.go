/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package prompts

import (
	_ "embed"
	"fmt"

	"github.com/mikeb26/octium/internal/types"
)

//go:embed system_msg.txt
var SystemMsgFmt string
var SystemMsg = fmt.Sprintf(SystemMsgFmt, types.RetrieveUrl,
	types.RetrieveUrl, types.ReadFile, types.FilePatch, types.FilePatch,
	types.CreateFile, types.AppendFile, types.CreateFile)

const SummarizeMsg = `Please summarize the entire prior conversation
history. The resulting summary should be optimized for consumption by a more
recent version of an LLM than yourself. The purpose of the summary is to manage
the limited size of the context window.`

const GitSummarizeMsg = `Please produce an appropriate git commit message by
summarizing the entire prior conversation history. The commit message should
adhere to best practices containing 1 short subject line, 1 empty line, and a
short paragraph description of the change being committed.`
