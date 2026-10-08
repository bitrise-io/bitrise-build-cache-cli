package trampoline

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// socketProbeTimeout is tight: a serving proxy answers within microseconds on
// a local unix socket; longer blocks here add latency to every compile.
const socketProbeTimeout = 50 * time.Millisecond

// startLockMaxWait caps the time EnsureProxy spends re-probing after losing the
// start lock. 1s is longer than a proxy boot takes in practice but short enough
// that an unhealthy start never deadlocks every compile.
const startLockMaxWait = time.Second

// startLockRetry is the sleep between re-probes while another process holds the
// start lock.
const startLockRetry = 10 * time.Millisecond

// socketPathEnv overrides the proxy socket location. Mirrors
// xceleratconfig.EnvProxySocketPath; duplicated here to keep trampoline free of
// internal/xcelerate/* imports.
const socketPathEnv = "BITRISE_XCELERATE_SOCKET_PATH"

const defaultSocketName = "xcelerate-proxy.sock"

const startLockFilename = "bitrise-xcelerate-starting.lock"

const cliBinaryName = "bitrise-build-cache"

// autostartDeps is the dependency bundle for EnsureProxy; tests swap atoms.
type autostartDeps struct {
	socketPath    func() string
	lockPath      func() string
	dial          func(path string, timeout time.Duration) error
	tryLock       func(path string) (release func(), ok bool, err error)
	startAutostep func()
	sleep         func(d time.Duration)
	now           func() time.Time
}

//nolint:gochecknoglobals // production dependency bundle; tests swap atoms.
var defaultAutostartDeps = autostartDeps{
	socketPath: func() string {
		if v := os.Getenv(socketPathEnv); v != "" {
			return v
		}

		return filepath.Join(os.TempDir(), defaultSocketName)
	},
	// Must match internal/paths.TrampolineStartLockFilename.
	lockPath: func() string { return filepath.Join(os.TempDir(), startLockFilename) },
	dial: func(path string, timeout time.Duration) error {
		conn, err := net.DialTimeout("unix", path, timeout)
		if err != nil {
			return err //nolint:wrapcheck // probe-only
		}
		_ = conn.Close()

		return nil
	},
	tryLock: func(path string) (func(), bool, error) {
		fl := flock.New(path)
		ok, err := fl.TryLock()
		if err != nil {
			return nil, false, err //nolint:wrapcheck
		}
		if !ok {
			return nil, false, nil
		}

		return func() { _ = fl.Unlock() }, true, nil
	},
	startAutostep: func() {
		// Fire-and-forget: the autostart is best-effort, the compiler must proceed
		// regardless and the proxy will be up on the next compile.
		cmd := exec.CommandContext(context.Background(), cliBinaryName, "xcelerate", "start-proxy") //nolint:noctx // intentionally detached: must outlive trampoline exec
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil
		_ = cmd.Start()
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}
	},
	sleep: time.Sleep,
	now:   time.Now,
}

// EnsureProxy probes the proxy socket; if absent, grabs the start lock and
// fires `bitrise-build-cache xcelerate start-proxy` detached. Returns quickly
// whether the probe succeeded or not — the compiler proceeds regardless.
func EnsureProxy() {
	ensureProxy(defaultAutostartDeps)
}

func ensureProxy(d autostartDeps) {
	if d.dial(d.socketPath(), socketProbeTimeout) == nil {
		return
	}

	release, ok, err := d.tryLock(d.lockPath())
	if err != nil {
		return
	}

	if !ok {
		// Another process is starting the proxy; re-probe briefly, then return.
		deadline := d.now().Add(startLockMaxWait)
		for d.now().Before(deadline) {
			if d.dial(d.socketPath(), socketProbeTimeout) == nil {
				return
			}
			d.sleep(startLockRetry)
		}

		return
	}

	defer release()

	d.startAutostep()
}
