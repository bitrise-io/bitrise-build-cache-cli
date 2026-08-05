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
	"syscall"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const pollInterval = 50 * time.Millisecond

// ErrHeld means a live process owns the lock; callers decide what to do about it.
var ErrHeld = errors.New("lock held by a live process")

type AliveFn func(pid int) bool

// The zero value fails fast and breaks open only on a dead owner.
type Options struct {
	// Zero fails immediately instead of blocking.
	Wait time.Duration
	// Only consulted when the marker carries no usable pid — liveness is exact,
	// this is a guess.
	TTL time.Duration
	// Reclaim keeps a re-entrant caller from blocking on itself.
	Reclaim bool
	IsAlive AliveFn
	Os      utils.OsProxy
}

// The returned release is always safe to call, including on the error paths.
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

// ReadOwner reads the marker's owner. A missing or malformed marker is (0, false).
func ReadOwner(osProxy utils.OsProxy, path string) (int, bool) {
	return Options{Os: osProxy}.readOwner(path)
}

// ClaimCooldown reports whether every has passed since the last claim, re-stamping
// when it returns true. A failure to stamp reads as "not claimed", so a broken
// marker stays quiet rather than spamming.
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

// A holder that died without releasing must not wedge every later caller.
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
