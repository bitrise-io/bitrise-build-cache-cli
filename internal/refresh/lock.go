package refresh

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockFile is a sibling of the registry — separate so the lock survives Save's atomic rename.
const LockFile = "refresh-registry.lock"

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
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
