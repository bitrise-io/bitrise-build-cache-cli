//go:build unit

package xcelerate

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/flock"
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

func TestStopProxy_NoProxyRunningIsSilent(t *testing.T) {
	home := t.TempDir()

	require.NoError(t, stopProxy(stopProxyDeps{
		logger:   nullLogger{},
		osProxy:  stubOsProxy{home: home},
		signaler: noopSignaler{},
	}))
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
