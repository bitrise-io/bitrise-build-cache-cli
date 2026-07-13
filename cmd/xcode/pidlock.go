package xcode

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// errProxyAlreadyRunning is returned by acquireProxyPidLock when a live proxy
// already owns the pid file.
var errProxyAlreadyRunning = errors.New("proxy already running")

// processAliveFn reports whether pid still names a running process.
type processAliveFn func(pid int) bool

//nolint:gochecknoglobals
var defaultProcessAlive processAliveFn = func(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return proc.Signal(syscall.Signal(0)) == nil
}

// readAlivePid returns the pid recorded in pidPath and whether the process is
// still running. Missing / malformed pid files return (0, false).
func readAlivePid(osProxy utils.OsProxy, pidPath string, isAlive processAliveFn) (int, bool) {
	if isAlive == nil {
		isAlive = defaultProcessAlive
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

// acquireProxyPidLock claims pidPath for the current process. Returns
// errProxyAlreadyRunning if a different live process already holds it. Stale
// or self-owned pid files are overwritten.
//
// The returned release closure removes the pid file — call it on shutdown.
func acquireProxyPidLock(osProxy utils.OsProxy, pidPath string, isAlive processAliveFn) (func() error, error) {
	pid, alive := readAlivePid(osProxy, pidPath, isAlive)
	if alive && pid != os.Getpid() {
		return nil, fmt.Errorf("%w (pid: %d)", errProxyAlreadyRunning, pid)
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
