/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package threads

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
	"testing"

	"github.com/mikeb26/octium/internal/fsatomic/local"
	"github.com/mikeb26/octium/internal/types"
)

func TestThreadGroupSet_Save(t *testing.T) {
	root := t.TempDir()

	tgs := NewThreadGroupSet(root, nil, local.New())

	tgs.mu.Lock()
	err := tgs.save(context.Background(), true)
	tgs.mu.Unlock()
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	path := filepath.Join(root, threadGroupSetFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %q to exist: %v", path, err)
	}
}

func TestThreadGroupSet_Load_MissingFile(t *testing.T) {
	root := t.TempDir()

	tgs := NewThreadGroupSet(root, nil, local.New())
	if err := tgs.Load(context.Background()); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// Defaults preserved.
	if tgs.persisted.ThreadNum != 0 {
		t.Fatalf("expected ThreadNum=0, got %v", tgs.persisted.ThreadNum)
	}
}

func TestThreadGroupSet_Load_RestoresFields(t *testing.T) {
	root := t.TempDir()

	// Write a persisted file with a non-default thread num.
	content, err := json.Marshal(&persistedThreadGroupSet{ThreadNum: 42})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	path := filepath.Join(root, threadGroupSetFileName)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	tgs := NewThreadGroupSet(root, nil, local.New())
	if err := tgs.Load(context.Background()); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if tgs.persisted.ThreadNum != 42 {
		t.Fatalf("expected ThreadNum=42, got %v", tgs.persisted.ThreadNum)
	}
}

func TestThreadGroupSet_NewThreadGroupSet_PrefixRules(t *testing.T) {
	root := t.TempDir()

	// NewThreadGroupSet no longer returns an error and no longer enforces
	// prefix validation. Ensure it constructs groups as requested.
	tgs := NewThreadGroupSet(root, []string{""}, local.New())
	if len(tgs.threadGrps) != 1 {
		t.Fatalf("expected 1 group, got %v", len(tgs.threadGrps))
	}
	if tgs.threadGrps[0].Name() != "" {
		t.Fatalf("expected group name to remain empty string, got %q", tgs.threadGrps[0].Name())
	}
}

func TestThreadGroupSet_NonIdleThreadCount_EmptySet(t *testing.T) {
	root := t.TempDir()

	tgs := NewThreadGroupSet(root, nil, local.New())
	if got := tgs.NonIdleThreadCount(); got != 0 {
		t.Fatalf("expected NonIdleThreadCount=0, got %v", got)
	}
}

func TestThreadGroupSet_NonIdleThreadCount_SumsAcrossGroups(t *testing.T) {
	root := t.TempDir()

	tgs := NewThreadGroupSet(root, []string{"grpA", "grpB"}, local.New())

	// Create some threads; new threads start idle.
	if err := tgs.NewThread(context.Background(), "grpA", "t1"); err != nil {
		t.Fatalf("NewThread failed: %v", err)
	}
	if err := tgs.NewThread(context.Background(), "grpA", "t2"); err != nil {
		t.Fatalf("NewThread failed: %v", err)
	}
	if err := tgs.NewThread(context.Background(), "grpB", "t3"); err != nil {
		t.Fatalf("NewThread failed: %v", err)
	}

	if got := tgs.NonIdleThreadCount(); got != 0 {
		t.Fatalf("expected NonIdleThreadCount=0 for all-idle threads, got %v", got)
	}

	// Mark one thread in grpA as running and one thread in grpB as blocked.
	// (Use the underlying map to avoid relying on sort order.)
	grpA := tgs.threadGrps[0]
	grpA.mu.RLock()
	if len(grpA.threads) != 2 {
		grpA.mu.RUnlock()
		t.Fatalf("expected 2 threads in grpA, got %v", len(grpA.threads))
	}
	for _, thr := range grpA.threads {
		thr.setStateIfEqual(ThreadStateIdle, ThreadStateRunning)
		break
	}
	grpA.mu.RUnlock()

	grpB := tgs.threadGrps[1]
	grpB.mu.RLock()
	if len(grpB.threads) != 1 {
		grpB.mu.RUnlock()
		t.Fatalf("expected 1 thread in grpB, got %v", len(grpB.threads))
	}
	for _, thr := range grpB.threads {
		thr.setStateIfEqual(ThreadStateIdle, ThreadStateBlocked)
		break
	}
	grpB.mu.RUnlock()

	if got := tgs.NonIdleThreadCount(); got != 2 {
		t.Fatalf("expected NonIdleThreadCount=2, got %v", got)
	}
}

func TestThreadGroupSet_Load_ReconcilesThreadNumFromDisk(t *testing.T) {
	root := t.TempDir()

	// Create a thread_group_set.json with a stale ThreadNum.
	content, err := json.Marshal(&persistedThreadGroupSet{ThreadNum: 0})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	path := filepath.Join(root, threadGroupSetFileName)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Create an on-disk thread with id=5 under the "main" group.
	grpDir := filepath.Join(root, "main")
	thrDir := filepath.Join(grpDir, "5")
	if err := os.MkdirAll(thrDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	base := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	thr := persistedThread{
		Name:       "existing",
		CreateTime: base,
		AccessTime: base,
		ModTime:    base,
		Dialogue:   []*types.ThreadMessage{},
		Id:         "5",
	}
	thrJSON, err := json.Marshal(&thr)
	if err != nil {
		t.Fatalf("marshal thread failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(thrDir, ThreadFileName), thrJSON, 0600); err != nil {
		t.Fatalf("write thread failed: %v", err)
	}

	tgs := NewThreadGroupSet(root, []string{"main"}, local.New())
	if err := tgs.Load(context.Background()); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// After load, new ids should start after the max on-disk id.
	if err := tgs.NewThread(context.Background(), "main", "new"); err != nil {
		t.Fatalf("NewThread failed: %v", err)
	}

	// Ensure the new thread's id is "6" (not "1").
	mainGrp := tgs.threadGrps[0]
	mainGrp.mu.RLock()
	_, has5 := mainGrp.threads["5"]
	_, has6 := mainGrp.threads["6"]
	mainGrp.mu.RUnlock()
	if !has5 {
		t.Fatalf("expected loaded thread id=5 to exist")
	}
	if !has6 {
		t.Fatalf("expected NewThread to allocate id=6")
	}
}
