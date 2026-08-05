// Package filelock provides cross-process coordination primitives built on
// O_EXCL marker files: a mutual-exclusion lock and a cooldown gate. Both are
// needed because the CLI is spawned as many short-lived parallel processes
// (Bazel spawns the credential helper per request, the xcodebuild wrapper starts
// the proxy) that have no other way to coordinate.
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
	"syscall"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const pollInterval = 50 * time.Millisecond

// ErrHeld means a live process owns the lock. Callers decide what that means:
// the proxy treats it as "someone else is already doing the job", the token
// refresh as "go ahead unserialised".
var ErrHeld = errors.New("lock held by a live process")

// AliveFn reports whether pid is still running.
type AliveFn func(pid int) bool

// Options selects the acquisition behaviour. The zero value fails fast, breaks
// open only on a dead owner, and talks to the real filesystem.
type Options struct {
	// Wait bounds how long to block for a held lock; zero fails immediately.
	Wait time.Duration
	// TTL breaks open a marker this old. Only consulted when the marker carries
	// no usable pid, since liveness is the exact answer and this is a guess.
	TTL time.Duration
	// Reclaim treats a marker this process already owns as free, so a re-entrant
	// caller is not blocked by itself.
	Reclaim bool
	IsAlive AliveFn
	Os      utils.OsProxy
}

// Acquire claims the marker at path. The returned release is always safe to
// call, including on the error paths.
func Acquire(ctx context.Context, path string, opts Options) (func() error, error) {
	noop := func() error { return nil }
	osProxy := opts.osProxy()

	if err := osProxy.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return noop, fmt.Errorf("create lock dir for %s: %w", path, err)
	}

	deadline := time.Now().Add(opts.Wait)
	for {
		claimed, err := opts.claim(path)
		if err != nil {
			return noop, err
		}
		if claimed {
			return func() error { return remove(osProxy, path) }, nil
		}

		if opts.breakOpen(path) {
			continue
		}

		if time.Now().After(deadline) {
			pid, _ := ReadOwner(osProxy, path)

			return noop, fmt.Errorf("%w: %s (pid %d)", ErrHeld, path, pid)
		}

		select {
		case <-ctx.Done():
			return noop, fmt.Errorf("waiting for lock %s: %w", path, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// ReadOwner returns the pid recorded in the marker at path and whether that
// process is still running. A missing or malformed marker reads as (0, false).
func ReadOwner(osProxy utils.OsProxy, path string) (int, bool) {
	return Options{Os: osProxy}.readOwner(path)
}

// ClaimCooldown reports whether at least every has passed since the marker at
// path was last claimed, re-stamping it when it returns true. Used to rate-limit
// output across separate processes; a failure to stamp reads as "not claimed"
// so a broken marker stays quiet rather than spamming.
func ClaimCooldown(path string, every time.Duration) bool {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false
	}

	if info, err := os.Stat(path); err == nil {
		if time.Since(info.ModTime()) < every {
			return false
		}
		now := time.Now()

		return os.Chtimes(path, now, now) == nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	_ = f.Close()

	return true
}

// Private — acquisition steps

// claim reports whether this process now owns the marker. O_EXCL is what makes
// concurrent claims pick exactly one winner.
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

// breakOpen removes a marker no live process owns, so a holder that died
// without releasing cannot wedge every later caller.
func (o Options) breakOpen(path string) bool {
	osProxy := o.osProxy()

	pid, alive := o.readOwner(path)
	selfOwned := o.Reclaim && pid == os.Getpid()
	switch {
	case alive && !selfOwned:
		return false
	case pid > 0:
		return remove(osProxy, path) == nil
	}

	// No usable pid: the writer died mid-write, or an older CLI wrote an empty
	// marker. Age is all that is left to go on.
	if o.TTL <= 0 {
		return false
	}
	info, err := osProxy.Stat(path)
	if err != nil || time.Since(info.ModTime()) <= o.TTL {
		return false
	}

	return remove(osProxy, path) == nil
}

func (o Options) readOwner(path string) (int, bool) {
	content, exists, err := o.osProxy().ReadFileIfExists(path)
	if err != nil || !exists {
		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(content))
	if err != nil || pid <= 0 {
		return 0, false
	}

	return pid, o.isAlive()(pid)
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

		return proc.Signal(syscall.Signal(0)) == nil
	}
}

func remove(osProxy utils.OsProxy, path string) error {
	if err := osProxy.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove lock %s: %w", path, err)
	}

	return nil
}
