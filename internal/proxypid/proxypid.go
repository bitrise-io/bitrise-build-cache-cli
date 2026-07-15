package proxypid

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

var ErrAlreadyRunning = errors.New("proxy already running")

type AliveFn func(pid int) bool

//nolint:gochecknoglobals
var defaultAlive AliveFn = func(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return proc.Signal(syscall.Signal(0)) == nil
}

// Read returns the pid recorded in pidPath and whether the process is still
// running. Missing / malformed pid files return (0, false).
func Read(osProxy utils.OsProxy, pidPath string, isAlive AliveFn) (int, bool) {
	if isAlive == nil {
		isAlive = defaultAlive
	}

	content, exists, err := osProxy.ReadFileIfExists(pidPath)
	if err != nil || !exists {
		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(content))
	if err != nil {
		return 0, false
	}

	if !isAlive(pid) {
		return pid, false
	}

	return pid, true
}

// Acquire claims pidPath for the current process. Returns ErrAlreadyRunning if
// a different live process already holds it. Stale or self-owned pid files are
// overwritten. The returned release closure removes the pid file.
func Acquire(osProxy utils.OsProxy, pidPath string, isAlive AliveFn) (func() error, error) {
	pid, alive := Read(osProxy, pidPath, isAlive)
	// Self-owned pid file is not a foreign owner — reclaim on same-process re-entry.
	if alive && pid != os.Getpid() {
		return nil, fmt.Errorf("%w (pid: %d)", ErrAlreadyRunning, pid)
	}

	if err := osProxy.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return nil, fmt.Errorf("write pid file: %w", err)
	}

	release := func() error {
		if err := osProxy.Remove(pidPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove pid file: %w", err)
		}

		return nil
	}

	return release, nil
}
