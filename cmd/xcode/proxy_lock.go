package xcode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofrs/flock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// ErrProxyAlreadyRunning means another process holds the proxy lock.
var ErrProxyAlreadyRunning = errors.New("xcelerate proxy already running")

// The file is both the lock and the pid advertisement: exclusion comes from the
// kernel lock, while stop-proxy and the xcodebuild wrapper read the pid out of it.
// It is never removed — unlinking a flock file lets two processes each believe they
// hold it.
func proxyPidFile(osProxy utils.OsProxy) string {
	return xcelerate.PathFor(osProxy, paths.ProxyPidFileName)
}

// acquireProxyLock claims the singleton. Contention is not an error the caller has
// to recover from: it means another proxy is already serving.
func acquireProxyLock(osProxy utils.OsProxy) (*flock.Flock, error) {
	path := proxyPidFile(osProxy)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create proxy pid dir: %w", err)
	}

	lock := flock.New(path)
	locked, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	if !locked {
		pid, _ := proxyOwner(osProxy)

		return nil, fmt.Errorf("%w (pid: %d)", ErrProxyAlreadyRunning, pid)
	}

	// Advertised after the lock is held, so a reader never sees a pid that does not
	// own the proxy.
	if err := osProxy.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		_ = lock.Unlock()

		return nil, fmt.Errorf("advertise proxy pid: %w", err)
	}

	return lock, nil
}

// proxyOwner reports the advertised pid and whether a proxy is holding the lock.
// Liveness is the lock itself: if it can be taken, nobody is serving.
func proxyOwner(osProxy utils.OsProxy) (int, bool) {
	path := proxyPidFile(osProxy)

	content, exists, err := osProxy.ReadFileIfExists(path)
	if err != nil || !exists {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(content))
	if err != nil || pid <= 0 {
		return 0, false
	}

	probe := flock.New(path)
	free, err := probe.TryLock()
	if err != nil {
		return pid, false
	}
	if free {
		_ = probe.Unlock()

		return pid, false
	}

	return pid, true
}
