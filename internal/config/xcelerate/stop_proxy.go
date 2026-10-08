package xcelerate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/flock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
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
// period. Returns nil (and logs) when no proxy is running. After stop, scans
// the proxy's log for `Enriched invocation PUT` lines emitted since the
// SIGTERM and surfaces a Visit URL per orphan to stdout.
func StopProxy(logger log.Logger, osProxy utils.OsProxy) error {
	return stopProxy(logger, osProxy, os.Stdout)
}

func stopProxy(logger log.Logger, osProxy utils.OsProxy, stdout io.Writer) error {
	pid, running := ProxyOwner(osProxy)
	if !running {
		logger.TDonef("No xcelerate-proxy is running")

		return nil
	}

	logger.TInfof("Stopping xcelerate-proxy...")

	if pid <= 0 {
		return fmt.Errorf("a proxy holds %s but advertised no usable pid", ProxyPidFile(osProxy))
	}

	stopSignalAt := time.Now()

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

	announceOrphanInvocations(osProxy, stopSignalAt, stdout, logger)

	return nil //nolint:nilerr // innerErr in the loop is the process-exit probe, not an operation failure
}

// enrichedPutRe matches the enricher's Infof line; must stay in sync with
// enrichment/enricher.go.
var enrichedPutRe = regexp.MustCompile(`Enriched invocation PUT ([a-f0-9-]+)`) //nolint:gochecknoglobals // compiled once

// announceOrphanInvocations best-effort scans the proxy's own log for orphan
// Enriched invocation PUT lines produced after stopSignalAt. For each unique
// ID, prints a Visit URL to stdout so operators get click-through from the
// stop-proxy command's output.
func announceOrphanInvocations(osProxy utils.OsProxy, stopSignalAt time.Time, stdout io.Writer, logger log.Logger) {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		logger.Debugf("Could not resolve home dir for orphan announcement: %s", err)

		return
	}

	p := paths.FromHome(home)
	logDir := p.XcelerateLogDir()

	entries, err := os.ReadDir(logDir)
	if err != nil {
		logger.Debugf("Could not read proxy log dir %s: %s", logDir, err)

		return
	}

	seen := make(map[string]struct{})
	cutoff := stopSignalAt.Add(-time.Second) // allow modest clock skew

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "-out.log") {
			continue
		}

		path := filepath.Join(logDir, entry.Name())
		info, err := entry.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			continue
		}

		scanLogForOrphanIDs(path, seen)
	}

	for id := range seen {
		fmt.Fprintf(stdout, "Invocation saved. Visit 👉 %s\n", enrichment.VisitURL(id))
	}
}

func scanLogForOrphanIDs(path string, seen map[string]struct{}) {
	body, err := os.ReadFile(path) //nolint:gosec // log file path under the user's own state dir
	if err != nil {
		return
	}

	for _, match := range enrichedPutRe.FindAllSubmatch(body, -1) {
		if len(match) < 2 { //nolint:mnd // group index
			continue
		}
		seen[string(match[1])] = struct{}{}
	}
}
