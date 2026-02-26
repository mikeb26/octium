/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package threads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mikeb26/octium/internal/types"
	"github.com/mikeb26/octium/internal/types/aiclient"
	"github.com/mikeb26/octium/internal/workspace"
	"github.com/negrel/assert"
)

const (
	ThreadNoExistErrFmt = "Thread %v does not exist. To list threads try 'ls'.\n"
	ThreadFileName      = "thread.json"
	ThreadScratchDir    = "scratch"
)

type ThreadState int

const (
	ThreadStateUnknown ThreadState = iota

	ThreadStateIdle
	ThreadStateRunning
	ThreadStateBlocked // e.g. waiting for user approval

	ThreadStateInvalid ThreadState = 2147483647
)

func (state ThreadState) String() string {
	switch state {
	case ThreadStateIdle:
		return "idle"
	case ThreadStateRunning:
		return "running"
	case ThreadStateBlocked:
		return "blocked"
	default:
	}

	return fmt.Sprintf("invalid <%v>", int(state))
}

type persistedThread struct {
	Name            string                 `json:"name"`
	CreateTime      time.Time              `json:"ctime"`
	AccessTime      time.Time              `json:"atime"`
	ModTime         time.Time              `json:"mtime"`
	Dialogue        []*types.ThreadMessage `json:"dialogue"`
	Metrics         ThreadMetrics          `json:"metrics,omitempty"`
	Id              string                 `json:"id3"`
	InvocationCount int                    `json:"inv_count"`
}

// ThreadMetrics are persisted per thread and accumulate over time.
//
// These are best-effort metrics; not all model adapters populate token usage.
type ThreadMetrics struct {
	TokenUsage TokenUsageMetrics `json:"token_usage,omitempty"`
}

// TokenUsageMetrics accumulates token usage across successful chat invocations.
type TokenUsageMetrics struct {
	Chats            int `json:"chats"`
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
}

type tokenUsageSnapshot struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ReasoningTokens  int
}

func (m *TokenUsageMetrics) addInvocation(u tokenUsageSnapshot) {
	m.Chats++
	m.PromptTokens += u.PromptTokens
	m.CompletionTokens += u.CompletionTokens
	m.TotalTokens += u.TotalTokens
	m.ReasoningTokens += u.ReasoningTokens
}

type Thread interface {
	State() ThreadState
	Id() string
	Name() string
	Metrics() ThreadMetrics
	Rename(string) error
	CreateTime() time.Time
	AccessTime() time.Time
	ModTime() time.Time
	Dialogue() []*types.ThreadMessage
	RenderBlocks() []RenderBlock
	Access() error
	Workspace() *workspace.Workspace
	ChatOnceAsync(context.Context, *types.InternalContext, string,
		bool, string) (*RunningThreadState, error)
}

type thread struct {
	persisted persistedThread

	oldDirName string
	dirName    string
	parentDir  string
	state      ThreadState
	runState   *RunningThreadState
	parent     *ThreadGroup

	// llmClient is created per-thread (and may be recreated as needed).
	llmClient aiclient.AIClient
	// needLLMReinit marks that this thread should recreate its per-thread
	// llmClient/approver on the next invocation.
	//
	// This is set when global LLM settings are changed (vendor/model/api-key/
	// audit-log), but we intentionally do not disrupt any currently running or
	// blocked invocation.
	needLLMReinit bool
	// asyncApprover is per-thread and is used to route approvals back to the UI
	// goroutine servicing this thread.
	asyncApprover *AsyncApprover
	ws            *workspace.Workspace
	mu            sync.RWMutex
}

// load restores a thread from disk.
//
// Thread persistence historically used a derived directory name
// (genUniqDirName). Newer versions persist threads under a directory name
// that matches the thread's id.
func (t *thread) load(ctx context.Context, parentDir string,
	dirName string) error {

	assert.Locked(&t.mu, "attempt to load thread %v/%v without holding thread mutex",
		parentDir, dirName)

	fullpath := filepath.Join(parentDir, dirName, ThreadFileName)
	threadFileText, err := os.ReadFile(fullpath)
	if err != nil {
		return fmt.Errorf("Failed to read %v: %w", fullpath, err)
	}

	if err := json.Unmarshal(threadFileText, &t.persisted); err != nil {
		return fmt.Errorf("Failed to parse %v: %w", fullpath, err)
	}

	t.state = ThreadStateIdle
	t.parentDir = parentDir

	loadedDirName := dirName
	if t.persisted.Id == "" {
		id, err := t.parent.parent.newThreadId(ctx)
		if err != nil {
			return err
		}
		t.persisted.Id = id
		err = t.save()
		if err != nil {
			return err
		}
	}

	t.dirName = t.persisted.Id
	if loadedDirName != t.dirName {
		t.oldDirName = loadedDirName
	}
	// Only initialize/load the workspace when the thread directory name already
	// matches the canonical thread id. During legacy directory migration we load
	// threads from a non-canonical directory name in order to discover their id,
	// and we later rename the directory. Creating the workspace under the
	// canonical id before the rename would cause the rename destination to
	// already exist and migration would fail.
	if t.oldDirName == "" {
		t.initWorkspace()
		t.loadWorkspaceBestEffort(ctx)
	}

	return nil
}

func (t *thread) initWorkspace() {
	assert.Locked(&t.mu, "attempt to init workspace for thread %v without holding thread mutex",
		t.persisted.Id)

	t.ws = workspace.New(t.scratchDir(), t.persisted.Id, t.parent.parent.scmClient)
}

func (t *thread) loadWorkspaceBestEffort(ctx context.Context) {
	assert.Locked(&t.mu, "attempt to load workspace for thread %v without holding thread mutex",
		t.persisted.Id)
	assert.NotNil(t.ws, "attempt to load nil workspace for thread %v", t.persisted.Id)

	err := t.ws.Load(ctx)
	if err == nil {
		return
	}
	// be resilient to invalid origin/sandbox and keep thread loading non-fatal.
	if errors.Is(err, workspace.ErrOriginRepoInvalid) {
		_ = t.ws.Reset()
		return
	}
	if errors.Is(err, workspace.ErrSandboxRepoInvalid) {
		_ = t.ws.ResetSandbox(ctx)
		return
	}

	// For any other workspace load errors (corruption, etc.), keep the thread
	// loadable and ensure a fresh ws.json exists for future operations.
	_ = t.ws.Save()
}

// State returns the current thread state. It is primarily intended for UI
// layers that want to render state (running/blocked/etc.).
func (t *thread) State() ThreadState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.state
}

// SetState sets the current thread state.
func (t *thread) setStateIfEqual(oldState, newState ThreadState) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.state == oldState {
		t.state = newState
	}
}

// Id returns the current thread id
func (t *thread) Id() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.persisted.Id
}

// Metrics returns the thread's cumulative persisted metrics.
//
// The returned struct is a copy; callers may mutate it without affecting the
// thread.
func (t *thread) Metrics() ThreadMetrics {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.persisted.Metrics
}

// CreateTime returns the thread creation timestamp.
func (t *thread) CreateTime() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.persisted.CreateTime
}

// AccessTime returns the last access timestamp.
func (t *thread) AccessTime() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.persisted.AccessTime
}

// ModTime returns the last modified timestamp.
func (t *thread) ModTime() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.persisted.ModTime
}

// Dialogue returns a deep copy of the thread's dialogue
func (t *thread) Dialogue() []*types.ThreadMessage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	orig := t.persisted.Dialogue
	dCopy := make([]*types.ThreadMessage, len(orig))
	copy(dCopy, orig)

	return dCopy
}

// Name returns the thread's name
func (t *thread) Name() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.persisted.Name
}

// Rename updates the thread's name and persists it to disk.
//
// Thread names are purely metadata: renaming does not change the thread id
// or its directory name.
func (t *thread) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrThreadNameRequired
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.state != ThreadStateIdle {
		return ErrThreadNotIdle
	}

	if t.persisted.Name == name {
		return nil
	}

	t.persisted.Name = name
	t.persisted.ModTime = time.Now()
	return t.save()
}

// save persists the thread's dialogue to a file; callers should already hold
// a write lock on the thread's mutex
func (t *thread) save() error {
	return t.saveWithDir(t.parentDir)
}
func (t *thread) saveWithDir(parentDir string) error {
	assert.Locked(&t.mu, "attempt to persist thread %v without holding thread mutex",
		t.persisted.Id)

	if t.state != ThreadStateIdle {
		return fmt.Errorf("cannot save non-idle thread state:%v", t.state)
	}

	threadFileContent, err := json.Marshal(&t.persisted)
	if err != nil {
		return fmt.Errorf("Failed to save thread %v: %w", t.persisted.Name,
			err)
	}

	threadDir := filepath.Join(parentDir, t.dirName)
	threadWorkDir := filepath.Join(threadDir, ThreadScratchDir)
	err = os.MkdirAll(threadWorkDir, 0700)
	if err != nil {
		return fmt.Errorf("Failed to save thread %v: %w", t.persisted.Name,
			err)
	}

	filePath := filepath.Join(threadDir, ThreadFileName)
	err = os.WriteFile(filePath, threadFileContent, 0600)
	if err != nil {
		return fmt.Errorf("Failed to save thread %v(%v): %w",
			t.persisted.Name, filePath, err)
	}

	return nil
}

func (t *thread) scratchDir() string {
	return filepath.Join(t.parentDir, t.dirName, ThreadScratchDir)
}

func (t *thread) Workspace() *workspace.Workspace {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.ws
}

// remove deletes the thread's persisted dialogue; callers should already hold
// a write lock on the thread's mutex
func (t *thread) remove() error {
	return t.removeWithDir(t.parentDir)
}
func (t *thread) removeWithDir(parentDir string) error {
	assert.Locked(&t.mu, "attempt to delete thread %v without holding thread mutex",
		t.persisted.Id)

	if t.state != ThreadStateIdle {
		return fmt.Errorf("cannot remove non-idle thread state:%v",
			t.state)
	}

	threadDir := filepath.Join(parentDir, t.dirName)
	err := os.RemoveAll(threadDir)
	if err != nil {
		return fmt.Errorf("Failed to delete thread %v(%v): %w",
			t.persisted.Name, threadDir, err)
	}

	return nil
}
