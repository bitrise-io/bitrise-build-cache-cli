// Package filelock provides cross-process coordination primitives built on
// O_EXCL marker files: a mutual-exclusion lock and a cooldown gate. Both are
// needed because the CLI is spawned as many short-lived parallel processes
// (Bazel spawns the credential helper per request) that have no other way to
// coordinate.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const pollInterval = 50 * time.Millisecond

// Acquire blocks until it owns the lock at path, ctx ends, or wait elapses. A
// holder that died without releasing is broken open once its marker is older
// than ttl. The returned release func is always safe to call, including on the
// error paths, where a non-nil error means "proceed unserialised".
func Acquire(ctx context.Context, path string, wait, ttl time.Duration) (func(), error) {
	noop := func() {}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return noop, fmt.Errorf("create lock dir for %s: %w", path, err)
	}

	deadline := time.Now().Add(wait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
			_ = f.Close()

			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return noop, fmt.Errorf("create lock %s: %w", path, err)
		}

		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > ttl {
			_ = os.Remove(path)

			continue
		}

		if time.Now().After(deadline) {
			return noop, fmt.Errorf("lock %s still held after %s", path, wait)
		}

		select {
		case <-ctx.Done():
			return noop, fmt.Errorf("waiting for lock %s: %w", path, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
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
