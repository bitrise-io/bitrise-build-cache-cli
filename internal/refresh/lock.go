package refresh

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockFile is the basename of the advisory-lock sibling next to the
// registry. Kept separate from the registry file so the lock survives
// the atomic rename Save performs.
const LockFile = "refresh-registry.lock"

// lockRegistry acquires an exclusive flock(2) on a sibling lockfile in
// the state dir. The returned unlock closure releases the lock and
// closes the file descriptor. Blocks until the lock is available — the
// only contenders are other CLI processes running on the same host, and
// the critical section is a small JSON read + write, so the wait stays
// in the millisecond range.
//
// Used by Mark to serialise read-modify-write registry updates so two
// parallel `bitrise-build-cache activate <tool>` invocations don't lose
// each other's entry.
func lockRegistry(home string) (func(), error) {
	dir := filepath.Join(home, StateDirRelative)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir for lock: %w", err)
	}

	path := filepath.Join(dir, LockFile)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644) //nolint:gosec // path is derived from home + constants
	if err != nil {
		return nil, fmt.Errorf("open registry lockfile: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()

		return nil, fmt.Errorf("flock registry lockfile: %w", err)
	}

	return func() {
		// LOCK_UN happens implicitly on Close, but flocking explicitly first
		// keeps the semantic obvious and gives a clean error path if the
		// runtime ever changes.
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
