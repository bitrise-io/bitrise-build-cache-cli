package xcode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
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

// proxyOwner reports whether a proxy is serving, and the pid it advertised.
//
// The lock is the authority; the pid is only for the message. Deciding on the pid
// first would report "not running" whenever the advertisement happens to be
// mid-write — WriteFile truncates before it fills — and that answer starts a
// second proxy.
func proxyOwner(osProxy utils.OsProxy) (int, bool) {
	path := proxyPidFile(osProxy)

	// Probing would create the file, so an absent one is answered without one.
	content, exists, err := osProxy.ReadFileIfExists(path)
	if err != nil || !exists {
		return 0, false
	}

	probe := flock.New(path)
	free, err := probe.TryLock()
	if err != nil {
		return 0, false
	}
	if free {
		_ = probe.Unlock()

		return 0, false
	}

	// Held, so a pid we cannot parse means "running, identity unknown".
	pid, err := strconv.Atoi(strings.TrimSpace(content))
	if err != nil || pid <= 0 {
		return 0, true
	}

	return pid, true
}

// withProxySingleton runs serve as the only proxy on this machine. Contention is
// not a failure: another proxy is already serving, so this one has nothing to do
// and says so rather than erroring.
//
// The only way to take the lock, so the policy cannot be bypassed by a future
// caller claiming it and deciding for itself.
func withProxySingleton(osProxy utils.OsProxy, logger log.Logger, serve func() error) error {
	path := proxyPidFile(osProxy)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create proxy pid dir: %w", err)
	}

	lock := flock.New(path)
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("lock %s: %w", path, err)
	}
	if !locked {
		pid, _ := proxyOwner(osProxy)
		logger.Infof("Skipping proxy startup: %s (pid: %d)", ErrProxyAlreadyRunning, pid)

		return nil
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			logger.Warnf("Failed to release proxy lock: %s", err)
		}
	}()

	// Advertised after the lock is held, so a reader never sees a pid that does not
	// own the proxy.
	if err := osProxy.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("advertise proxy pid: %w", err)
	}

	return serve()
}
