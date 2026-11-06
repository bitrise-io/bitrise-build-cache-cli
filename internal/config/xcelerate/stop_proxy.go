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
// See cmd/xcode/proxy_lock.go (withProxySingleton) for the layering rationale.
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
// sends SIGTERM to the proxy pid directly, waits for exit, then escalates to
// SIGKILL. Returns nil (and logs) when no proxy is running.
func StopProxy(logger log.Logger, osProxy utils.OsProxy) error {
	return stopProxy(stopProxyDeps{
		logger:   logger,
		osProxy:  osProxy,
		signaler: realSignaler{},
	})
}

// stopProxyDeps bundles the external calls stopProxy makes. Tests swap signaler
// for a fake that neither sends real signals nor waits on process exit.
type stopProxyDeps struct {
	logger   log.Logger
	osProxy  utils.OsProxy
	signaler proxySignaler
}

// proxySignaler sends SIGTERM to the proxy pid, waits up to graceful for it to
// exit, then escalates to SIGKILL. Debug-logs failures; no return value.
type proxySignaler interface {
	SignalAndWait(pid int, graceful time.Duration, logger log.Logger)
}

type realSignaler struct{}

// Trampoline-spawned proxies inherit the trampoline's pgid, so a `-pid` signal
// misses the proxy entirely. Target the pid directly; after the grace window,
// escalate to SIGKILL and warn if the proxy is still alive.
func (realSignaler) SignalAndWait(pid int, graceful time.Duration, logger log.Logger) {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		logger.Debugf("kill (TERM) failed: %s", err)
	}

	timeout := time.After(graceful)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			_ = syscall.Kill(pid, syscall.SIGKILL)
			if err := syscall.Kill(pid, 0); err == nil {
				logger.Warnf("xcelerate-proxy pid %d still alive after SIGKILL", pid)
			}

			return
		case <-ticker.C:
			if err := syscall.Kill(pid, 0); err != nil {
				return
			}
		}
	}
}

func stopProxy(d stopProxyDeps) error {
	pid, running := ProxyOwner(d.osProxy)
	if !running {
		d.logger.TDonef("No xcelerate-proxy is running")

		return nil
	}

	d.logger.TInfof("Stopping xcelerate-proxy...")

	if pid <= 0 {
		return fmt.Errorf("a proxy holds %s but advertised no usable pid", ProxyPidFile(d.osProxy))
	}

	d.signaler.SignalAndWait(pid, 5*time.Second, d.logger)

	d.logger.TDonef("Stopped xcelerate-proxy")

	return nil
}
