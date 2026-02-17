/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package threads

import (
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
	"time"

	"github.com/mikeb26/octium/internal/types"
)

const (
	CodeBlockDelim        = "```"
	CodeBlockDelimNewline = "```\n"
)

// RenderBlockSource identifies who produced a block of text.
//
// This is UI-agnostic so that different frontends (classic CLI, ncurses,
// etc.) can render the same logical content with their own styling.
type RenderBlockSource int

const (
	RenderBlockSourceUser RenderBlockSource = iota
	RenderBlockSourceAssistant
)

// RenderBlock represents a contiguous span of text with a particular
// semantic role. It does not contain any ANSI color or formatting
// information; callers are expected to style it appropriately.
type RenderBlock struct {
	Source RenderBlockSource
	IsCode bool
	Text   string
}

// RenderBlocks flattens the thread dialogue into a sequence of
// RenderBlocks that capture the semantic structure (user prompt,
// assistant text, assistant code) without imposing any particular UI
// representation.
func (t *thread) RenderBlocks() []RenderBlock {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return RenderBlocksFromDialogue(t.persisted.Dialogue)
}

// RenderBlocksFromDialogue flattens a dialogue into a sequence of
// RenderBlocks that capture the semantic structure (user prompt,
// assistant text, assistant code) without imposing any particular UI
// representation.
//
// System messages are omitted.
func RenderBlocksFromDialogue(dialogue []*types.ThreadMessage) []RenderBlock {
	blocks := make([]RenderBlock, 0)

	for _, msg := range dialogue {
		if msg == nil {
			continue
		}
		if msg.Role == types.LlmRoleSystem {
			continue
		}

		parts := splitBlocks(msg.Content)
		src := RenderBlockSourceUser
		if msg.Role == types.LlmRoleAssistant {
			src = RenderBlockSourceAssistant
		}
		for idx, p := range parts {
			blocks = append(blocks, RenderBlock{
				IsCode: idx%2 == 1,
				Source: src,
				Text:   p,
			})
		}
	}

	return blocks
}

func genUniqDirName(name string, cTime time.Time) string {
	return fmt.Sprintf("%v_%v",
		strconv.FormatUint(uint64(crc32.ChecksumIEEE([]byte(name))), 16),
		cTime.Unix())
}

func splitBlocks(text string) []string {
	blocks := make([]string, 0)

	inBlock := false
	idx := strings.Index(text, CodeBlockDelim)
	numBlocks := 0
	for ; idx != -1; idx = strings.Index(text, CodeBlockDelim) {
		appendText := text[0:idx]
		if inBlock {
			appendText = CodeBlockDelim + appendText
		} else if numBlocks != 0 {
			blocks[numBlocks-1] = blocks[numBlocks-1] + CodeBlockDelim
		}
		blocks = append(blocks, appendText)
		text = text[idx+len(CodeBlockDelim):]
		inBlock = !inBlock
		numBlocks++
	}
	if len(text) > 0 {
		if inBlock {
			// Unclosed code fence: render it as literal text by re-attaching the
			// opening delimiter.
			text = text + CodeBlockDelim
		} else if numBlocks != 0 {
			// We consumed a closing delimiter; attach it to the preceding code
			// segment so the fence is preserved in the rendered output.
			blocks[numBlocks-1] = blocks[numBlocks-1] + CodeBlockDelim
		}
		blocks = append(blocks, text)
		return blocks
	}

	// If the message ended immediately after a closing delimiter, we still need
	// to re-attach that delimiter to the preceding segment.
	if !inBlock && numBlocks != 0 {
		blocks[numBlocks-1] = blocks[numBlocks-1] + CodeBlockDelim
	}

	return blocks
}
