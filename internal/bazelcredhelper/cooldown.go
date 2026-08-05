package bazelcredhelper

import (
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// claimCooldown reports whether every has passed since the last claim, re-stamping
// the marker when it returns true. The marker's mtime is the state; nothing is
// removed. A failure to stamp reads as "not claimed", so a broken marker stays
// quiet rather than spamming.
//
// The lock is what makes it a rate limiter rather than a suggestion: read-then-stamp
// is not atomic, and every helper of a build starts in the same instant, so without
// it they all claim the same window.
func claimCooldown(path string, every time.Duration) bool {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false
	}

	lock := flock.New(path + ".lock")
	held, err := lock.TryLock()
	if err != nil || !held {
		return false
	}
	defer func() { _ = lock.Unlock() }()

	if info, err := os.Stat(path); err == nil {
		if time.Since(info.ModTime()) < every {
			return false
		}
		now := time.Now()

		return os.Chtimes(path, now, now) == nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	_ = f.Close()

	return true
}
