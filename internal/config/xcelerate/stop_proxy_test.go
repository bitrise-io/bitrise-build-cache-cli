//go:build unit

package xcelerate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type stubOsProxy struct {
	utils.DefaultOsProxy

	home string
}

func (s stubOsProxy) UserHomeDir() (string, error) { return s.home, nil }

type noopSignaler struct{}

func (noopSignaler) SignalAndWait(_ int, _ time.Duration, _ log.Logger) {}

type capturingSignaler struct {
	pids []int
}

func (c *capturingSignaler) SignalAndWait(pid int, _ time.Duration, _ log.Logger) {
	c.pids = append(c.pids, pid)
}

// setupProxyPidFile writes a pid file and holds the flock for the test's
// lifetime so ProxyOwner returns (pid, true).
func setupProxyPidFile(t *testing.T, home string, pid int) {
	t.Helper()
	pidPath := filepath.Join(home, paths.XcelerateRootRelative, paths.ProxyPidFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0o755))
	require.NoError(t, os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644))

	lock := flock.New(pidPath)
	locked, err := lock.TryLock()
	require.NoError(t, err)
	require.True(t, locked, "test holds the flock so ProxyOwner reports running")
	t.Cleanup(func() { _ = lock.Unlock() })
}

func TestStopProxy_RunningProxyInvokesSignaler(t *testing.T) {
	home := t.TempDir()
	setupProxyPidFile(t, home, 12345)

	require.NoError(t, stopProxy(stopProxyDeps{
		logger:   nullLogger{},
		osProxy:  stubOsProxy{home: home},
		signaler: noopSignaler{},
	}))
}

func TestStopProxy_SignalsPositivePID(t *testing.T) {
	home := t.TempDir()
	setupProxyPidFile(t, home, 54321)

	sig := &capturingSignaler{}
	require.NoError(t, stopProxy(stopProxyDeps{
		logger:   nullLogger{},
		osProxy:  stubOsProxy{home: home},
		signaler: sig,
	}))

	require.Len(t, sig.pids, 1)
	assert.Positive(t, sig.pids[0], "signaler must receive the positive proxy pid, not a process-group id")
	assert.Equal(t, 54321, sig.pids[0])
}

func TestStopProxy_NoProxyRunningIsSilent(t *testing.T) {
	home := t.TempDir()

	require.NoError(t, stopProxy(stopProxyDeps{
		logger:   nullLogger{},
		osProxy:  stubOsProxy{home: home},
		signaler: noopSignaler{},
	}))
}

// TestRealSignaler_SIGKILLEscalation proves the fallback path works: a child
// that ignores SIGTERM must still be killed by SIGKILL within the grace window.
func TestRealSignaler_SIGKILLEscalation(t *testing.T) {
	// Shell ignores TERM then loops in-process (no forked `sleep`), so the pid
	// we signal IS the pid that must die.
	cmd := exec.Command("sh", "-c", `trap "" TERM; while :; do read -t 30 _ </dev/null || true; done`)
	require.NoError(t, cmd.Start())
	// Give the shell time to install the TERM trap before signalling.
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	realSignaler{}.SignalAndWait(cmd.Process.Pid, 100*time.Millisecond, nullLogger{})
	// Reap the child so its pid stops reading as "alive" (zombie) in kill(pid, 0).
	waited, werr := cmd.Process.Wait()
	require.NoError(t, werr)

	require.Less(t, time.Since(start), 500*time.Millisecond, "SignalAndWait + reap must complete within 500ms")

	status, ok := waited.Sys().(syscall.WaitStatus)
	require.True(t, ok)
	assert.True(t, status.Signaled(), "child must have exited via signal, not normal return")
	assert.Equal(t, syscall.SIGKILL, status.Signal(), "SIGTERM was trapped; only SIGKILL could land")
}

type nullLogger struct{}

func (nullLogger) Printf(string, ...any)  {}
func (nullLogger) Donef(string, ...any)   {}
func (nullLogger) Infof(string, ...any)   {}
func (nullLogger) Warnf(string, ...any)   {}
func (nullLogger) Errorf(string, ...any)  {}
func (nullLogger) Debugf(string, ...any)  {}
func (nullLogger) TPrintf(string, ...any) {}
func (nullLogger) TDonef(string, ...any)  {}
func (nullLogger) TInfof(string, ...any)  {}
func (nullLogger) TWarnf(string, ...any)  {}
func (nullLogger) TErrorf(string, ...any) {}
func (nullLogger) TDebugf(string, ...any) {}
func (nullLogger) Println()               {}
func (nullLogger) EnableDebugLog(bool)    {}
