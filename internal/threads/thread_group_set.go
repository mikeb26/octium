/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package threads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/mikeb26/octium/internal/fsatomic"
	"github.com/mikeb26/octium/internal/scm"
	"github.com/negrel/assert"
)

const (
	threadGroupSetFileName = "thread_group_set.json"
)

type persistedThreadGroupSet struct {
	ThreadNum int64 `json:"thread_num"`
}

// ThreadGroupSet is a concurrency-safe container for 0 or more ThreadGroups.
//
// It also owns persisted metadata that is shared across its ThreadGroups.
//
// NOTE: The persistence file is written directly under dir, so the
// ThreadGroupSet dir MUST NOT be the same directory as any ThreadGroup dir
// that contains thread JSON files.
type ThreadGroupSet struct {
	persisted    persistedThreadGroupSet
	persistedVer fsatomic.Version

	dir        string
	fileName   string
	threadGrps []*ThreadGroup
	afs        fsatomic.AtomicFS
	scmClient  scm.Client

	mu sync.RWMutex
}

// ResetLLMClients marks all threads in the set for lazy per-thread llmClient
// reinitialization.
//
// This is intended to be called when the user changes global LLM settings
// (vendor/model/key/audit). We do not disrupt any currently running or blocked
// thread; instead, the next time a thread transitions from idle->running, it
// will recreate its per-thread llmClient and async approver.
func (tgs *ThreadGroupSet) ResetLLMClients() {
	tgs.mu.RLock()
	defer tgs.mu.RUnlock()

	for _, tg := range tgs.threadGrps {
		tg.resetLLMClients()
	}
}

func NewThreadGroupSet(dirIn string, thrGroupNames []string,
	afsIn fsatomic.AtomicFS, scmClientIn scm.Client) *ThreadGroupSet {

	set := &ThreadGroupSet{
		persisted:  persistedThreadGroupSet{ThreadNum: 0},
		dir:        dirIn,
		fileName:   threadGroupSetFileName,
		threadGrps: make([]*ThreadGroup, 0),
		afs:        afsIn,
		scmClient:  scmClientIn,
	}

	for _, thrGroupName := range thrGroupNames {
		grpDir := filepath.Join(dirIn, thrGroupName)
		thrGrp := newThreadGroup(set, thrGroupName, grpDir)
		set.threadGrps = append(set.threadGrps, thrGrp)
	}

	return set
}

// NewThread creates a new thread in the specified thread group
func (tgs *ThreadGroupSet) NewThread(ctx context.Context, thrGroupName string,
	thrName string) error {

	tgs.mu.Lock()
	defer tgs.mu.Unlock()

	for _, thrGroup := range tgs.threadGrps {
		if thrGroup.Name() == thrGroupName {
			return thrGroup.NewThread(ctx, thrName)
		}
	}

	return fmt.Errorf("No such thread group %v", thrGroupName)
}

func (tgs *ThreadGroupSet) MoveThread(ctx context.Context, thr Thread, srcThrGrpName, dstThrGrpName string) error {
	tgs.mu.Lock()
	defer tgs.mu.Unlock()

	var srcThrGrp, dstThrGrp *ThreadGroup
	for _, thrGroup := range tgs.threadGrps {
		if thrGroup.Name() == srcThrGrpName {
			srcThrGrp = thrGroup
		}
		if thrGroup.Name() == dstThrGrpName {
			dstThrGrp = thrGroup
		}
	}

	if srcThrGrp == nil {
		return fmt.Errorf("No such thread group %v", srcThrGrpName)
	}
	if dstThrGrp == nil {
		return fmt.Errorf("No such thread group %v", dstThrGrpName)
	}

	return srcThrGrp.MoveThread(ctx, thr, dstThrGrp)
}

// newThreadId generated a new, monotonically increasing, persistent thread id.
// callers should already hold a write lock on the thread group set's mutex
func (tgs *ThreadGroupSet) newThreadId(ctx context.Context) (string, error) {

	assert.Locked(&tgs.mu, "attempt to add thread without holding thread group set %v mutex",
		tgs.dir)

	var err error
	for {
		tgs.persisted.ThreadNum++
		err = tgs.save(ctx, false)
		if errors.Is(err, fsatomic.ErrConflict) {
			err = tgs.readPersisted(ctx)
			if err == nil {
				continue
			}
		}
		break
	}

	if err != nil {
		return "", fmt.Errorf("failed to save thread group set: %w", err)
	}

	return strconv.FormatInt(tgs.persisted.ThreadNum, 10), nil
}

func (tgs *ThreadGroupSet) Threads(thrGroupNames []string) []Thread {
	tgs.mu.RLock()
	defer tgs.mu.RUnlock()

	ret := make([]Thread, 0)
	for _, thrGroup := range tgs.threadGrps {
		match := len(thrGroupNames) == 0
		for _, matchThrGroupName := range thrGroupNames {
			if thrGroup.Name() == matchThrGroupName {
				match = true
				break
			}
		}
		if !match {
			continue
		}

		ret = append(ret, thrGroup.Threads()...)
	}

	slices.SortFunc(ret, func(a, b Thread) int {
		return -a.AccessTime().Compare(b.AccessTime())
	})

	return ret
}

func (tgs *ThreadGroupSet) NonIdleThreadCount() int {
	tgs.mu.RLock()
	defer tgs.mu.RUnlock()

	count := 0
	for _, thrGroup := range tgs.threadGrps {
		count += thrGroup.NonIdleThreadCount()
	}

	return count
}

// Load restores persisted thread group set.
func (tgs *ThreadGroupSet) Load(ctx context.Context) error {
	tgs.mu.Lock()
	defer tgs.mu.Unlock()

	err := tgs.readPersisted(ctx)
	if err != nil {
		return err
	}

	if err := tgs.loadThreadGroups(ctx); err != nil {
		return err
	}

	// Ensure ThreadNum never lags behind the highest on-disk thread id.
	//
	// The thread_group_set.json file can be missing/reset (e.g. user deletes it,
	// old versions didn't create it, restoring from backup, etc.).
	return tgs.reconcileThreadNumFromLoadedThreads(ctx)
}

func (tgs *ThreadGroupSet) reconcileThreadNumFromLoadedThreads(ctx context.Context) error {
	assert.Locked(&tgs.mu, "attempt to reconcile thread group set %v without holding mutex",
		tgs.dir)

	found := make(map[int64]*ThreadGroup)
	maxID := tgs.persisted.ThreadNum
	for _, tg := range tgs.threadGrps {
		tg.mu.RLock()
		for id := range tg.threads {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				continue
			}
			_, ok := found[n]
			if ok {
				panic(fmt.Sprintf("Thread id %v in both %v and %v", n,
					found[n].name, tg.name))
			}
			found[n] = tg
			if n > maxID {
				maxID = n
			}
		}
		tg.mu.RUnlock()
	}

	if maxID <= tgs.persisted.ThreadNum {
		return nil
	}

	// CAS loop in case another process updates thread_group_set.json while we're
	// loading.
	for {
		tgs.persisted.ThreadNum = maxID
		err := tgs.save(ctx, false)
		if errors.Is(err, fsatomic.ErrConflict) {
			if err := tgs.readPersisted(ctx); err != nil {
				return err
			}
			if tgs.persisted.ThreadNum >= maxID {
				return nil
			}
			continue
		}
		return err
	}
}

func (tgs *ThreadGroupSet) readPersisted(ctx context.Context) error {
	assert.Locked(&tgs.mu, "attempt to read thread group set without holding %v mutex",
		tgs.dir)

	filePath := filepath.Join(tgs.dir, tgs.fileName)

	content, f, err := tgs.afs.ReadFile(ctx, filePath)
	if err != nil {
		if errors.Is(err, fsatomic.ErrNotFound) {
			return tgs.save(ctx, true)
		}
		return fmt.Errorf("failed to read thread group set (%v): %w", filePath,
			err)
	}

	var persisted persistedThreadGroupSet
	if err := json.Unmarshal(content, &persisted); err != nil {
		return fmt.Errorf("failed to parse thread group set (%v): %w", filePath,
			err)
	}
	if persisted.ThreadNum < 0 {
		persisted.ThreadNum = 0
	}
	tgs.persisted = persisted
	tgs.persistedVer = f.Version

	return nil
}

// save persists the thread group set fields to disk; callers should already
// hold a write lock on the thread group set's mutex.
func (tgs *ThreadGroupSet) save(ctx context.Context, force bool) error {
	assert.Locked(&tgs.mu, "attempt to persist thread group set %v without holding mutex",
		tgs.dir)

	content, err := json.Marshal(&tgs.persisted)
	if err != nil {
		return fmt.Errorf("failed to marshal thread group set: %w", err)
	}

	filePath := filepath.Join(tgs.dir, tgs.fileName)
	var f fsatomic.File
	if force {
		f, err = tgs.afs.WriteFile(ctx, filePath, content, 0o600)
	} else {
		f, err = tgs.afs.WriteFileCAS(ctx, filePath, tgs.persistedVer, content, 0o600)
	}
	if err != nil {
		return fmt.Errorf("failed to save thread group set (%v): %w", filePath, err)
	}
	tgs.persistedVer = f.Version

	return nil
}

// loadThreadGroups loads threads for each thread group in the set. callers
// should already hold a write lock on the thread group set's mutex.
func (tgs *ThreadGroupSet) loadThreadGroups(ctx context.Context) error {
	assert.Locked(&tgs.mu, "attempt to load thread groups holding thread group set %v mutex",
		tgs.dir)

	for _, tg := range tgs.threadGrps {
		if err := tg.LoadThreads(ctx); err != nil {
			return err
		}
	}

	return nil
}
