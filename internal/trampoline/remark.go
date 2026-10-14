package trampoline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// Must match internal/paths.XcelerateRootRelative + ProxyPidFileName. Duplicated
// here to keep trampoline free of internal/xcelerate/* and internal/paths imports.
const (
	proxyPidRelative     = ".bitrise-xcelerate/proxy.pid"
	remarkSentinelPrefix = "bitrise-xcelerate-remark-"
	remarkText           = "remark: Bitrise remote build cache engaged"
)

const startTimeLookupTimeout = 200 * time.Millisecond

// remarkDeps lets tests swap pid lookup, start-time lookup, sentinel creation, and stderr.
type remarkDeps struct {
	readProxyPid   func() (int, bool)
	proxyStartTime func(pid int) (int64, bool)
	createSentinel func(path string) (ok bool, err error)
	stderr         io.Writer
	tmpDir         func() string
}

//nolint:gochecknoglobals // production dependency bundle; tests swap atoms.
var defaultRemarkDeps = remarkDeps{
	readProxyPid: func() (int, bool) {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, false
		}

		return readPidFile(filepath.Join(home, proxyPidRelative))
	},
	proxyStartTime: lookupProcessStartTimeMS,
	createSentinel: func(path string) (bool, error) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) //nolint:gosec // tmp-dir sentinel
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				return false, nil
			}

			return false, err //nolint:wrapcheck
		}
		_ = f.Close()

		return true, nil
	},
	stderr: os.Stderr,
	tmpDir: os.TempDir,
}

// EmitEngagedRemark prints the Xcode-surfaced engagement remark exactly once
// per proxy session. Shim must only call it AFTER a successful proxy socket
// probe — a missing pid file means no proxy is up yet and the remark is skipped.
func EmitEngagedRemark() {
	emitEngagedRemark(defaultRemarkDeps)
}

func emitEngagedRemark(d remarkDeps) {
	pid, ok := d.readProxyPid()
	if !ok {
		return
	}

	sentinel := filepath.Join(d.tmpDir(), sentinelName(pid, d.proxyStartTime))
	created, err := d.createSentinel(sentinel)
	if err != nil || !created {
		return
	}

	fmt.Fprintln(d.stderr, remarkText)
}

// sentinelName encodes pid plus proxy start-time-ms so a recycled PID across a
// proxy restart doesn't collide with a stale sentinel. Falls back to pid-only
// when start-time is unresolvable (accepts the stale-pid edge case; /tmp is
// swept on reboot and a lingering sentinel just silences one remark).
func sentinelName(pid int, lookup func(int) (int64, bool)) string {
	if lookup != nil {
		if start, ok := lookup(pid); ok {
			return remarkSentinelPrefix + strconv.Itoa(pid) + "-" + strconv.FormatInt(start, 10)
		}
	}

	return remarkSentinelPrefix + strconv.Itoa(pid)
}

func lookupProcessStartTimeMS(pid int) (int64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), startTimeLookupTimeout)
	defer cancel()

	proc, err := process.NewProcessWithContext(ctx, int32(pid)) //nolint:gosec // PIDs fit in int32
	if err != nil {
		return 0, false
	}

	ct, err := proc.CreateTimeWithContext(ctx)
	if err != nil {
		return 0, false
	}

	return ct, true
}

func readPidFile(path string) (int, bool) {
	raw, err := os.ReadFile(path) //nolint:gosec // caller-controlled path under user's own home
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))

	return pid, err == nil && pid > 0
}
