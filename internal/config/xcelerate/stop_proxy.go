package xcelerate

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/flock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/urllog"
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
// sends SIGTERM to the process group, and escalates to SIGKILL after a grace
// period. Returns nil (and logs) when no proxy is running. Reads the per-proxy
// URL log before and after SIGTERM to print a Visit URL per orphan the proxy
// emitted (both pre-shutdown and during the final sweep), then truncates it.
func StopProxy(logger log.Logger, osProxy utils.OsProxy) error {
	return stopProxy(stopProxyDeps{
		logger:   logger,
		osProxy:  osProxy,
		stdout:   os.Stdout,
		signaler: realSignaler{},
	})
}

// stopProxyDeps bundles the external calls stopProxy makes. Tests swap
// signaler for a fake that neither sends real signals nor waits on process
// exit, and may use afterSignalHook to simulate an orphan URL arriving during
// the shutdown window.
type stopProxyDeps struct {
	logger          log.Logger
	osProxy         utils.OsProxy
	stdout          io.Writer
	signaler        proxySignaler
	afterSignalHook func()
}

// proxySignaler sends SIGTERM to -pid, waits up to graceful for the group to
// exit, then sends SIGKILL. Debug-logs failures; no return value.
type proxySignaler interface {
	SignalAndWait(pid int, graceful time.Duration, logger log.Logger)
}

type realSignaler struct{}

func (realSignaler) SignalAndWait(pid int, graceful time.Duration, logger log.Logger) {
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		logger.Debugf("kill (TERM) failed: %s", err)
	}

	timeout := time.After(graceful)
	tick := time.Tick(200 * time.Millisecond)
	for {
		select {
		case <-timeout:
			_ = syscall.Kill(-pid, syscall.SIGKILL)

			return
		case <-tick:
			if err := syscall.Kill(-pid, 0); err != nil {
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

	urlLogPath := invocationURLsPath(d.osProxy, pid, d.logger)

	seen := make(map[string]struct{})
	printNewURLs(urlLogPath, seen, d.stdout, d.logger)

	d.signaler.SignalAndWait(pid, 5*time.Second, d.logger)

	if d.afterSignalHook != nil {
		d.afterSignalHook()
	}

	d.logger.TDonef("Stopped xcelerate-proxy")

	printNewURLs(urlLogPath, seen, d.stdout, d.logger)

	if urlLogPath != "" {
		if err := urllog.Delete(urlLogPath); err != nil {
			d.logger.Debugf("Failed to delete urllog %s: %s", urlLogPath, err)
		}
	}

	return nil
}

// invocationURLsPath resolves the per-proxy urllog file via internal/paths.
// Returns "" when the home dir cannot be resolved — the caller skips URL output.
func invocationURLsPath(osProxy utils.OsProxy, pid int, logger log.Logger) string {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		logger.Debugf("Could not resolve home dir for emitted-URL log: %s", err)

		return ""
	}

	return paths.FromHome(home).InvocationURLsForPID(pid)
}

// printNewURLs reads the urllog and prints a Visit URL for any ID not already
// in seen, which it then updates. Missing file / read errors are debug-logged.
func printNewURLs(path string, seen map[string]struct{}, stdout io.Writer, logger log.Logger) {
	if path == "" {
		return
	}

	recs, err := urllog.Read(path)
	if err != nil {
		logger.Debugf("Failed to read emitted-URL log %s: %s", path, err)

		return
	}

	for _, rec := range recs {
		if _, dup := seen[rec.InvocationID]; dup {
			continue
		}
		seen[rec.InvocationID] = struct{}{}
		fmt.Fprintf(stdout, "Invocation saved. Visit 👉 %s\n", enrichment.VisitURL(rec.InvocationID))
	}
}
