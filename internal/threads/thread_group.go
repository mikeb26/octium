/* Copyright © 2023-2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package threads

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/mikeb26/gptcli/internal/types"
	"github.com/negrel/assert"
)

type ThreadGroup struct {
	name       string
	threads    map[string]*thread
	totThreads int
	dir        string
	mu         sync.RWMutex
	parent     *ThreadGroupSet
}

func newThreadGroup(parentIn *ThreadGroupSet, nameIn string,
	dirIn string) *ThreadGroup {

	thrGrp := &ThreadGroup{
		name:       nameIn,
		threads:    make(map[string]*thread),
		totThreads: 0,
		dir:        dirIn,
		parent:     parentIn,
	}

	return thrGrp
}

func (thrGrp *ThreadGroup) Threads() []Thread {
	thrGrp.mu.RLock()
	defer thrGrp.mu.RUnlock()

	out := make([]Thread, 0, len(thrGrp.threads))
	for _, thr := range thrGrp.threads {
		out = append(out, thr)
	}

	slices.SortFunc(out, func(a, b Thread) int {
		return -a.AccessTime().Compare(b.AccessTime())
	})

	return out
}

func (thrGrp *ThreadGroup) Name() string {
	thrGrp.mu.RLock()
	defer thrGrp.mu.RUnlock()

	return thrGrp.name
}

// NonIdleThreadCount returns the number of threads in the group that are not
// idle (e.g. running or blocked waiting for user approval).
//
// This is intended for UI layers that want to warn the user before quitting.
func (thrGrp *ThreadGroup) NonIdleThreadCount() int {
	thrGrp.mu.RLock()
	defer thrGrp.mu.RUnlock()

	count := 0
	for _, thr := range thrGrp.threads {
		if thr == nil {
			continue
		}
		if thr.State() != ThreadStateIdle {
			count++
		}
	}

	return count
}

func (thrGrp *ThreadGroup) hasNonIdleThreads() bool {
	// caller holds thrGrp.mu so each thread's state cannot transition
	// out of idle.
	for _, thr := range thrGrp.threads {
		if thr.State() != ThreadStateIdle {
			return true
		}
	}

	return false
}

func (thrGrp *ThreadGroup) migrateOldThreadDirs(ctx context.Context) error {
	assert.Locked(&thrGrp.mu, "attempt to migrate old thread dirs %v without holding thread group mutex",
		thrGrp.dir)

	// Legacy directory names contain an underscore (crc32hex_unixtime) while
	// canonical ids are monotonically increasing integers.
	// (see genUniqDirName())
	dEntries, err := os.ReadDir(thrGrp.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("Failed to read dir %v: %w", thrGrp.dir, err)
	}

	for _, dEnt := range dEntries {
		if !dEnt.IsDir() {
			continue
		}
		if !strings.Contains(dEnt.Name(), "_") {
			continue
		}

		// load the thread to ensure valid thread id
		curThread := &thread{parent: thrGrp}
		curThread.mu.Lock()
		err := curThread.load(ctx, thrGrp.dir, dEnt.Name())
		curThread.mu.Unlock()
		if err != nil {
			return err
		}

		srcPath := filepath.Join(thrGrp.dir, dEnt.Name())
		dstPath := filepath.Join(thrGrp.dir, curThread.dirName)
		if srcPath == dstPath {
			continue
		}
		if _, err := os.Stat(dstPath); err == nil {
			return fmt.Errorf("cannot migrate legacy thread dir %q -> %q: destination exists",
				srcPath, dstPath)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to stat thread dir %q: %w", dstPath, err)
		}

		if err := os.Rename(srcPath, dstPath); err != nil {
			return fmt.Errorf("failed to migrate thread dir %q -> %q: %w",
				srcPath, dstPath, err)
		}
	}

	return nil
}

func (thrGrp *ThreadGroup) LoadThreads(ctx context.Context) error {
	thrGrp.mu.Lock()
	defer thrGrp.mu.Unlock()

	if thrGrp.hasNonIdleThreads() {
		return fmt.Errorf("Cannot re-load thread group with non-idle threads")
	}

	err := thrGrp.migrateOldThreadDirs(ctx)
	if err != nil {
		return err
	}

	thrGrp.totThreads = 0
	thrGrp.threads = make(map[string]*thread)

	dEntries, err := os.ReadDir(thrGrp.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("Failed to read dir %v: %w", thrGrp.dir, err)
	}

	for _, dEnt := range dEntries {
		if !dEnt.IsDir() {
			continue
		}

		curThread := &thread{parent: thrGrp}
		curThread.mu.Lock()
		err := curThread.load(ctx, thrGrp.dir, dEnt.Name())
		curThread.mu.Unlock()
		if err != nil {
			return err
		}
		if curThread.oldDirName != "" {
			return fmt.Errorf("thread dir name %q does not match id %q after migration",
				curThread.oldDirName, curThread.dirName)
		}

		thrId := curThread.persisted.Id
		if thrId == "" {
			return fmt.Errorf("loaded thread missing id")
		}
		if _, exists := thrGrp.threads[thrId]; exists {
			return fmt.Errorf("duplicate thread id %q while loading from %q",
				thrId, filepath.Join(thrGrp.dir, dEnt.Name()))
		}
		thrGrp.threads[thrId] = curThread
	}
	thrGrp.totThreads = len(thrGrp.threads)

	return nil
}

// activateThread updates the thread group's current thread state,
// refreshes the access time, and persists the thread to disk. It
// performs no user-facing I/O and is therefore safe to call from
// different UIs (CLI, ncurses, etc.).
func (thread *thread) Access() error {
	thread.mu.Lock()
	defer thread.mu.Unlock()

	thread.persisted.AccessTime = time.Now()
	// Persisting a running/blocked thread would fail (and is generally not
	// desirable) because the thread may be actively mutating in the background.
	//
	// Allow UIs to "activate" (focus) a non-idle thread so they can
	// detach/reattach to a running thread without failing here.
	if thread.state == ThreadStateIdle {
		if err := thread.save(); err != nil {
			return err
		}
	}

	return nil
}

// NewThread encapsulates the logic to allocate and register a new
// thread in the main thread group. It is used both by the CLI "new"
// subcommand and the ncurses menu UI so their behavior stays in sync.
func (thrGrp *ThreadGroup) NewThread(ctx context.Context, name string) error {
	thrGrp.mu.Lock()
	defer thrGrp.mu.Unlock()

	cTime := time.Now()

	dialogue := []*types.ThreadMessage{}

	curThread := &thread{
		persisted: persistedThread{
			Name:       name,
			CreateTime: cTime,
			AccessTime: cTime,
			ModTime:    cTime,
			Dialogue:   dialogue,
		},
		dirName:   "",
		parentDir: thrGrp.dir,
		state:     ThreadStateIdle,
		parent:    thrGrp,
	}
	id, err := thrGrp.parent.newThreadId(ctx)
	if err != nil {
		return err
	}
	curThread.persisted.Id = id
	curThread.dirName = id

	thrGrp.addThread(curThread)

	return nil
}

func (thrGrp *ThreadGroup) addThread(curThread *thread) {
	if curThread.persisted.Id == "" {
		panic("missing thread id")
	}
	if _, exists := thrGrp.threads[curThread.persisted.Id]; exists {
		panic(fmt.Sprintf("attempt to add thread id %v to group %v but it already exists",
			curThread.persisted.Id, thrGrp.name))
	}
	thrGrp.threads[curThread.persisted.Id] = curThread
	thrGrp.totThreads++
}

// @todo need ux
//  unarchiveThreadMain()

func (thrGrp *ThreadGroup) Count() int {
	thrGrp.mu.RLock()
	defer thrGrp.mu.RUnlock()

	return thrGrp.totThreads
}

func (srcThrGrp *ThreadGroup) MoveThread(thr Thread, dstThrGrp *ThreadGroup) error {
	if srcThrGrp == dstThrGrp {
		return fmt.Errorf("cannot move thread within the same thread group")
	}

	// Ensure consistent locking order to prevent deadlocks if two goroutines
	// concurrently move threads in opposite directions between two groups.
	first, second := srcThrGrp, dstThrGrp
	if uintptr(unsafe.Pointer(first)) > uintptr(unsafe.Pointer(second)) {
		first, second = second, first
	}

	first.mu.Lock()
	defer first.mu.Unlock()
	second.mu.Lock()
	defer second.mu.Unlock()

	thrId := thr.Id()
	if _, exists := srcThrGrp.threads[thrId]; !exists {
		return fmt.Errorf("thread %q does not exist in group %q", thrId,
			srcThrGrp.name)
	}

	thread := srcThrGrp.threads[thrId]
	thread.mu.Lock()
	defer thread.mu.Unlock()

	if thread.state != ThreadStateIdle {
		// Avoid calling thread.State() while holding thread.mu.
		return fmt.Errorf("cannot move non-idle thread %q (state: %v): %w",
			thrId, thread.state, ErrThreadNotIdle)
	}

	err := thread.saveWithDir(dstThrGrp.dir)
	if err != nil {
		return err
	}
	err = thread.removeWithDir(srcThrGrp.dir)
	if err != nil {
		_ = thread.removeWithDir(dstThrGrp.dir)
		return err
	}

	thread.parentDir = dstThrGrp.dir
	dstThrGrp.addThread(thread)

	delete(srcThrGrp.threads, thrId)
	srcThrGrp.totThreads--

	return nil
}
