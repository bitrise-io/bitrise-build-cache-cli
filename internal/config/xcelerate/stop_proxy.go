package xcelerate

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/flock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// ProxyPidFile returns the path of the xcelerate proxy pid/lock file. The file
// is both the pid advertisement and the exclusion lock — never removed even after
// the proxy exits, because unlinking would let two proxies each hold their own inode.
func ProxyPidFile(osProxy utils.OsProxy) string {
	return PathFor(osProxy, paths.ProxyPidFileName)
}

// ProxyOwner reports whether a proxy is serving, and the pid it advertised.
// See cmd/xcode/proxy_lock.go for the layering rationale.
func ProxyOwner(osProxy utils.OsProxy) (int, bool) {
	path := ProxyPidFile(osProxy)

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

	pid, err := strconv.Atoi(strings.TrimSpace(content))
	if err != nil || pid <= 0 {
		return 0, true
	}

	return pid, true
}

// StopProxy stops the xcelerate proxy: it discovers the pid from the lock file,
// sends SIGTERM to the process group, and escalates to SIGKILL after a grace
// period. Returns nil (and logs) when no proxy is running.
func StopProxy(logger log.Logger, osProxy utils.OsProxy) error {
	logger.TInfof("Stopping xcelerate-proxy...")

	pid, running := ProxyOwner(osProxy)
	if !running {
		logger.TDonef("No xcelerate-proxy is running")

		return nil
	}
	if pid <= 0 {
		return fmt.Errorf("a proxy holds %s but advertised no usable pid", ProxyPidFile(osProxy))
	}

	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		logger.Debugf("kill (TERM) failed: %s", err)
	}

	timeout := time.After(5 * time.Second)
	tick := time.Tick(200 * time.Millisecond)
loop:
	for {
		select {
		case <-timeout:
			break loop
		case <-tick:
			if innerErr := syscall.Kill(-pid, 0); innerErr != nil {
				break loop
			}
		}
	}

	_ = syscall.Kill(-pid, syscall.SIGKILL)

	logger.TDonef("Stopped xcelerate-proxy")

	return nil //nolint:nilerr // innerErr in the loop is the process-exit probe, not an operation failure
}
