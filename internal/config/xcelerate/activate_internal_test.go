//go:build unit

package xcelerate

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestWriteExecutableAtomically_OverwritesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, cliBasename)
	require.NoError(t, os.WriteFile(target, []byte("OLD-BINARY"), 0o755))

	require.NoError(t, writeExecutableAtomically(dir, target, strings.NewReader("NEW-BINARY")))

	got, err := os.ReadFile(target) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "NEW-BINARY", string(got))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	assertNoTempFilesLeftBehind(t, dir)
}

// Regression for the "corrupted cli left behind" failure: a copy that fails
// partway must not touch the already-installed binary. The old in-place
// O_TRUNC write truncated the target before failing; the atomic rename leaves
// it untouched.
func TestWriteExecutableAtomically_FailedCopyLeavesTargetIntact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, cliBasename)
	require.NoError(t, os.WriteFile(target, []byte("GOOD-INSTALLED-BINARY"), 0o755))

	err := writeExecutableAtomically(dir, target, &failingReader{data: []byte("partial")})
	require.Error(t, err)

	got, err := os.ReadFile(target) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "GOOD-INSTALLED-BINARY", string(got),
		"a failed copy must not corrupt or truncate the installed binary")

	assertNoTempFilesLeftBehind(t, dir)
}

func TestWriteExecutableAtomically_CreatesTargetWhenMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, cliBasename)

	require.NoError(t, writeExecutableAtomically(dir, target, strings.NewReader("FRESH")))

	got, err := os.ReadFile(target) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "FRESH", string(got))

	assertNoTempFilesLeftBehind(t, dir)
}

func assertNoTempFilesLeftBehind(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "temp file left behind: %s", e.Name())
	}
}

type failingReader struct {
	data []byte
	read bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, errors.New("simulated read failure")
	}

	r.read = true
	n := copy(p, r.data)

	return n, nil
}

var _ io.Reader = (*failingReader)(nil)

func TestEnsureLogDir_CreatesWhatTheProxyWouldCreateLazily(t *testing.T) {
	home := t.TempDir()
	logDir := paths.FromHome(home).XcelerateLogDir()
	require.NoDirExists(t, logDir)

	ensureLogDir(log.NewLogger(), &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		MkdirAllFunc:    os.MkdirAll,
	})

	assert.DirExists(t, logDir)
}

func TestEnsureLogDir_FailureDoesNotStopActivation(t *testing.T) {
	ensureLogDir(log.NewLogger(), &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return "", errors.New("no home") },
		MkdirAllFunc:    func(string, os.FileMode) error { return errors.New("read-only fs") },
	})
}

func swapEnsureFn(t *testing.T, fn func(context.Context, log.Logger, []daemonpkg.Service, daemonpkg.EnsureDeps) error) {
	t.Helper()
	prev := ensureFn
	ensureFn = fn
	t.Cleanup(func() { ensureFn = prev })
}

// The proxy is never supervised: a launchd job lands in its own resource
// coalition and loses to the compiler it serves. The xcodebuild wrapper forks
// it instead. See docs/daemon-latency.md.
func TestRunDaemonEnsure_wiresNoXcelerateProxyService(t *testing.T) {
	var gotServices []daemonpkg.Service
	swapEnsureFn(t, func(_ context.Context, _ log.Logger, services []daemonpkg.Service, _ daemonpkg.EnsureDeps) error {
		gotServices = services

		return nil
	})

	err := runDaemonEnsure(t.Context(), log.NewLogger(), map[string]string{}, false)
	require.NoError(t, err)

	if runtime.GOOS == "darwin" {
		assert.Empty(t, gotServices)
	} else {
		require.Len(t, gotServices, 1)
		assert.Equal(t, daemonpkg.ServiceXcelerateProxy, gotServices[0].Name)
	}
}

func TestRunDaemonEnsure_forwardsSkipEnvVarViaEnvs(t *testing.T) {
	var gotEnvs map[string]string
	swapEnsureFn(t, func(_ context.Context, _ log.Logger, _ []daemonpkg.Service, deps daemonpkg.EnsureDeps) error {
		gotEnvs = deps.Envs

		return nil
	})

	envs := map[string]string{daemonpkg.EnvSkipEnsure: "1"}

	err := runDaemonEnsure(t.Context(), log.NewLogger(), envs, false)
	require.NoError(t, err)
	assert.Equal(t, "1", gotEnvs[daemonpkg.EnvSkipEnsure])
}

func TestXcelerateRunDaemonEnsure_propagatesEnsureError(t *testing.T) {
	swapEnsureFn(t, func(context.Context, log.Logger, []daemonpkg.Service, daemonpkg.EnsureDeps) error {
		return errors.New("launchctl bootstrap: permission denied")
	})

	err := runDaemonEnsure(t.Context(), log.NewLogger(), map[string]string{}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

// A launchd job is started by the service manager, not the shell, so the flag
// has to travel through EnsureDeps or the daemon can never log at debug level.
func TestRunDaemonEnsure_forwardsDebugLogging(t *testing.T) {
	var gotDebug bool
	swapEnsureFn(t, func(_ context.Context, _ log.Logger, _ []daemonpkg.Service, deps daemonpkg.EnsureDeps) error {
		gotDebug = deps.DebugLogging

		return nil
	})

	require.NoError(t, runDaemonEnsure(t.Context(), log.NewLogger(), map[string]string{}, true))
	assert.True(t, gotDebug)
}
