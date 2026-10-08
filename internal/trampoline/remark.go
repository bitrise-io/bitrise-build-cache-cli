package trampoline

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofrs/flock"
)

// Must match internal/paths.XcelerateRootRelative + ProxyPidFileName. Duplicated
// here to keep trampoline free of internal/xcelerate/* and internal/paths imports.
const (
	proxyPidRelative     = ".bitrise-xcelerate/proxy.pid"
	remarkSentinelPrefix = "bitrise-xcelerate-remark-"
	remarkText           = "remark: Bitrise remote build cache engaged"
)

// remarkDeps lets tests swap the pid lookup, sentinel lock, and stderr writer.
type remarkDeps struct {
	readProxyPid func() (int, bool)
	tryLock      func(path string) (ok bool, err error)
	stderr       io.Writer
	tmpDir       func() string
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
	tryLock: func(path string) (bool, error) {
		fl := flock.New(path)
		ok, err := fl.TryLock()
		if err != nil {
			return false, err //nolint:wrapcheck
		}

		return ok, nil
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

	sentinel := filepath.Join(d.tmpDir(), remarkSentinelPrefix+strconv.Itoa(pid))
	locked, err := d.tryLock(sentinel)
	if err != nil || !locked {
		return
	}

	fmt.Fprintln(d.stderr, remarkText)
}

func readPidFile(path string) (int, bool) {
	raw, err := os.ReadFile(path) //nolint:gosec // caller-controlled path under user's own home
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, false
		}

		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, false
	}

	return pid, true
}
