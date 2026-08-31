package xcode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/flock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// ErrProxyAlreadyRunning means another process holds the proxy lock.
var ErrProxyAlreadyRunning = errors.New("xcelerate proxy already running")

// proxyPidFile is a cmd-local alias for xcelerate.ProxyPidFile, kept so tests in
// this package can address it without importing the internal xcelerate package.
func proxyPidFile(osProxy utils.OsProxy) string {
	return xcelerate.ProxyPidFile(osProxy)
}

// proxyOwner is a cmd-local alias for xcelerate.ProxyOwner.
func proxyOwner(osProxy utils.OsProxy) (int, bool) {
	return xcelerate.ProxyOwner(osProxy)
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
