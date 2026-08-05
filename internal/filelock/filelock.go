// Package filelock coordinates the CLI's short-lived parallel processes — Bazel
// spawns the credential helper per request — which have no other way to serialise.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	pollInterval     = 50 * time.Millisecond
	stealGuardSuffix = ".steal"
	// A steal is a read and an unlink, so a guard older than this belongs to a
	// process that died holding it.
	stealGuardTTL = 5 * time.Second
	// Bounds how many corpses in a row one call will break open, so a stream of
	// crashing holders cannot keep Acquire past its Wait.
	maxSteals = 8
)

// ErrHeld means a live process owns the lock; callers decide what to do about it.
var ErrHeld = errors.New("lock held by a live process")

type AliveFn func(pid int) bool

// The zero value fails fast and breaks open only on a dead owner.
type Options struct {
	// Zero fails immediately instead of blocking.
	Wait time.Duration
	// TTL breaks open a marker with no usable pid, where age is all there is to
	// go on.
	TTL time.Duration
	// MaxHold is the last-resort ceiling on a marker whose pid still probes as
	// alive. Pids get recycled and zombies answer signal 0, so without it such a
	// marker is held forever. Set it well above any legitimate hold: breaking one
	// open concedes that two processes may run the critical section at once.
	MaxHold time.Duration
	// Reclaim lets a caller through a marker carrying its own pid that it is not
	// currently holding.
	Reclaim bool
	IsAlive AliveFn
	Os      utils.OsProxy
}

// Markers this process holds right now. Reclaim consults it so a second
// in-process Acquire cannot be granted a lock the first still owns.
var mine = struct { //nolint:gochecknoglobals
	sync.Mutex

	paths map[string]bool
}{paths: map[string]bool{}}

// The returned release is always safe to call, including on the error paths.
func Acquire(ctx context.Context, path string, opts Options) (func() error, error) {
	noop := func() error { return nil }

	if err := opts.osProxy().MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return noop, fmt.Errorf("create lock dir for %s: %w", path, err)
	}

	deadline := time.Now().Add(opts.Wait)
	steals := 0
	for {
		claimed, err := opts.claim(path)
		if err != nil {
			return noop, err
		}
		if claimed {
			hold(path)

			return func() error { return opts.release(path) }, nil
		}

		stolen := opts.breakOpen(path)

		// Checked on both paths: a successful steal used to jump straight back to
		// the claim, ignoring cancellation and the caller's budget.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return noop, fmt.Errorf("waiting for lock %s: %w", path, ctxErr)
		}
		if stolen {
			if steals++; steals < maxSteals {
				continue
			}

			return noop, fmt.Errorf("%w: %s (broke open %d markers without claiming it)", ErrHeld, path, steals)
		}

		if time.Now().After(deadline) {
			pid, _ := ReadOwner(opts.osProxy(), path)

			return noop, fmt.Errorf("%w: %s (pid %d)", ErrHeld, path, pid)
		}

		select {
		case <-ctx.Done():
			return noop, fmt.Errorf("waiting for lock %s: %w", path, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// ReadOwner reads the marker's owner. A missing or malformed marker is (0, false).
func ReadOwner(osProxy utils.OsProxy, path string) (int, bool) {
	return Options{Os: osProxy}.readOwner(path)
}

// ClaimCooldown reports whether every has passed since the last claim, re-stamping
// when it returns true. A failure to stamp reads as "not claimed", so a broken
// marker stays quiet rather than spamming.
func ClaimCooldown(path string, every time.Duration) bool {
	opts := Options{}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false
	}

	// Read-then-stamp is not atomic, so without this every process that starts in
	// the same instant claims the same window and the output is not rate-limited
	// at all.
	guard := path + stealGuardSuffix
	f, err := os.OpenFile(guard, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		opts.dropAbandonedGuard(guard)

		return false
	}
	_ = f.Close()
	defer func() { _ = os.Remove(guard) }()

	if info, err := os.Stat(path); err == nil {
		if time.Since(info.ModTime()) < every {
			return false
		}
		now := time.Now()

		return os.Chtimes(path, now, now) == nil
	}

	marker, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	_ = marker.Close()

	return true
}

// O_EXCL is what makes concurrent claims pick exactly one winner.
func (o Options) claim(path string) (bool, error) {
	f, err := o.osProxy().OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
		_ = f.Close()

		return true, nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return false, fmt.Errorf("create lock %s: %w", path, err)
	}

	return false, nil
}

// release refuses to unlink a marker that is no longer ours: removing a live
// successor's marker would admit a third holder and cascade.
func (o Options) release(path string) error {
	defer unhold(path)

	if pid := o.markerPID(path); pid != 0 && pid != os.Getpid() {
		return fmt.Errorf("lock %s now held by pid %d, not removing", path, pid)
	}

	return remove(o.osProxy(), path)
}

// A holder that died without releasing must not wedge every later caller.
func (o Options) breakOpen(path string) bool {
	if !o.stealable(path) {
		return false
	}

	return o.steal(path)
}

// stealable reports whether the marker is a corpse — or, under Reclaim, ours.
func (o Options) stealable(path string) bool {
	pid, alive := o.readOwner(path)
	switch {
	case pid > 0 && !alive:
		return true
	case pid > 0 && o.Reclaim && pid == os.Getpid() && !holding(path):
		return true
	case pid > 0:
		return o.olderThan(path, o.MaxHold)
	}

	return o.olderThan(path, o.TTL)
}

// steal admits one process at a time to the destructive path, then takes the
// verdict again: the caller's was read outside the guard, and another stealer may
// have replaced the corpse with its own live marker since. Unlinking on a stale
// verdict would hand the lock to two owners at once.
func (o Options) steal(path string) bool {
	osProxy := o.osProxy()
	guard := path + stealGuardSuffix

	f, err := osProxy.OpenFile(guard, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		o.dropAbandonedGuard(guard)

		return false
	}
	_ = f.Close()
	defer func() { _ = remove(osProxy, guard) }()

	if !o.stealable(path) {
		return false
	}

	return remove(osProxy, path) == nil
}

// A stealer that died mid-steal must not wedge break-open for good.
func (o Options) dropAbandonedGuard(guard string) {
	if o.olderThan(guard, stealGuardTTL) {
		_ = remove(o.osProxy(), guard)
	}
}

func (o Options) olderThan(path string, age time.Duration) bool {
	if age <= 0 {
		return false
	}
	info, err := o.osProxy().Stat(path)

	return err == nil && time.Since(info.ModTime()) > age
}

func (o Options) readOwner(path string) (int, bool) {
	pid := o.markerPID(path)
	if pid <= 0 {
		return 0, false
	}

	return pid, o.isAlive()(pid)
}

func (o Options) markerPID(path string) int {
	content, exists, err := o.osProxy().ReadFileIfExists(path)
	if err != nil || !exists {
		return 0
	}

	pid, err := strconv.Atoi(strings.TrimSpace(content))
	if err != nil || pid <= 0 {
		return 0
	}

	return pid
}

func (o Options) osProxy() utils.OsProxy {
	if o.Os == nil {
		return utils.DefaultOsProxy{}
	}

	return o.Os
}

func (o Options) isAlive() AliveFn {
	if o.IsAlive != nil {
		return o.IsAlive
	}

	return func(pid int) bool {
		proc, err := o.osProxy().FindProcess(pid)
		if err != nil {
			return false
		}

		return aliveFromSignalErr(proc.Signal(syscall.Signal(0)))
	}
}

// EPERM means the process is there but owned by another user, which is still a
// live holder. Only "no such process" makes a marker stealable.
func aliveFromSignalErr(err error) bool {
	return err == nil || errors.Is(err, fs.ErrPermission)
}

func hold(path string) {
	mine.Lock()
	defer mine.Unlock()
	mine.paths[path] = true
}

func unhold(path string) {
	mine.Lock()
	defer mine.Unlock()
	delete(mine.paths, path)
}

func holding(path string) bool {
	mine.Lock()
	defer mine.Unlock()

	return mine.paths[path]
}

func remove(osProxy utils.OsProxy, path string) error {
	if err := osProxy.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove lock %s: %w", path, err)
	}

	return nil
}
