//go:build unit

package xcelerate

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/urllog"
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

func stopProxyTestDeps(t *testing.T, home string, afterSignalHook func()) (*bytes.Buffer, stopProxyDeps) {
	t.Helper()
	buf := &bytes.Buffer{}

	return buf, stopProxyDeps{
		logger:          nullLogger{},
		osProxy:         stubOsProxy{home: home},
		stdout:          buf,
		signaler:        noopSignaler{},
		afterSignalHook: afterSignalHook,
	}
}

func TestStopProxy_PrintsURLsFromFixturedLog(t *testing.T) {
	home := t.TempDir()
	setupProxyPidFile(t, home, 12345)

	urlPath := paths.FromHome(home).InvocationURLsForPID(12345)
	w := &urllog.Writer{Path: urlPath}
	require.NoError(t, w.Append("id-a"))
	require.NoError(t, w.Append("id-b"))

	buf, deps := stopProxyTestDeps(t, home, nil)
	require.NoError(t, stopProxy(deps))

	out := buf.String()
	assert.Equal(t, 2, strings.Count(out, "Invocation saved. Visit"))
	assert.Contains(t, out, "https://app.bitrise.io/build-cache/invocations/xcode/id-a")
	assert.Contains(t, out, "https://app.bitrise.io/build-cache/invocations/xcode/id-b")

	_, err := os.Stat(urlPath)
	assert.True(t, os.IsNotExist(err), "urllog must be deleted after stop-proxy completes")
}

func TestStopProxy_EmptyFilePrintsNothing(t *testing.T) {
	home := t.TempDir()
	setupProxyPidFile(t, home, 22222)

	urlPath := paths.FromHome(home).InvocationURLsForPID(22222)
	require.NoError(t, os.MkdirAll(filepath.Dir(urlPath), 0o755))
	require.NoError(t, os.WriteFile(urlPath, nil, 0o644))

	buf, deps := stopProxyTestDeps(t, home, nil)
	require.NoError(t, stopProxy(deps))

	assert.NotContains(t, buf.String(), "Invocation saved")
}

func TestStopProxy_MissingFileIsSilent(t *testing.T) {
	home := t.TempDir()
	setupProxyPidFile(t, home, 33333)

	buf, deps := stopProxyTestDeps(t, home, nil)
	require.NoError(t, stopProxy(deps))

	assert.NotContains(t, buf.String(), "Invocation saved")
}

func TestStopProxy_URLsAppendedDuringShutdownAreIncluded(t *testing.T) {
	home := t.TempDir()
	setupProxyPidFile(t, home, 44444)

	urlPath := paths.FromHome(home).InvocationURLsForPID(44444)
	w := &urllog.Writer{Path: urlPath}
	require.NoError(t, w.Append("id-before"))

	hook := func() {
		_ = w.Append("id-during-shutdown")
	}

	buf, deps := stopProxyTestDeps(t, home, hook)
	require.NoError(t, stopProxy(deps))

	out := buf.String()
	assert.Equal(t, 2, strings.Count(out, "Invocation saved. Visit"))
	assert.Contains(t, out, "id-before")
	assert.Contains(t, out, "id-during-shutdown")
}

func TestStopProxy_NoProxyRunningSkipsEverything(t *testing.T) {
	home := t.TempDir()

	buf, deps := stopProxyTestDeps(t, home, nil)
	require.NoError(t, stopProxy(deps))

	assert.NotContains(t, buf.String(), "Invocation saved")
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
