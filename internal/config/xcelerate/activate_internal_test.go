//go:build unit

package xcelerate

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
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

// swapEnsureFn replaces the daemon.Ensure seam for the duration of a test,
// letting us observe (pushChanged, services) without touching launchctl.
func swapEnsureFn(t *testing.T, fn func(context.Context, log.Logger, []daemonpkg.Service, bool, daemonpkg.EnsureDeps) error) {
	t.Helper()
	prev := ensureFn
	ensureFn = fn
	t.Cleanup(func() { ensureFn = prev })
}

func TestReadExistingPushEnabled_missingConfig_returnsFalse(t *testing.T) {
	// No config file — hadOldConfig must be false so callers don't try to
	// diff against a non-existent baseline.
	osProxy := &utilsMocks.OsProxyMock{
		OpenFileFunc: func(string, int, os.FileMode) (*os.File, error) { return nil, os.ErrNotExist },
		UserHomeDirFunc: func() (string, error) { return t.TempDir(), nil },
	}

	oldPush, existed := readExistingPushEnabled(osProxy, utils.DefaultDecoderFactory{}, map[string]string{})
	assert.False(t, existed)
	assert.False(t, oldPush)
}

func TestRunDaemonEnsure_pushChangedFlagPropagates(t *testing.T) {
	cases := []struct {
		name        string
		pushChanged bool
	}{
		{name: "push flipped → pushChanged=true", pushChanged: true},
		{name: "push unchanged → pushChanged=false", pushChanged: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPush bool
			var gotServices []daemonpkg.Service
			swapEnsureFn(t, func(_ context.Context, _ log.Logger, services []daemonpkg.Service, pushChanged bool, _ daemonpkg.EnsureDeps) error {
				gotServices = services
				gotPush = pushChanged

				return nil
			})

			osProxy := &utilsMocks.OsProxyMock{
				OpenFileFunc: func(string, int, os.FileMode) (*os.File, error) { return nil, os.ErrNotExist },
				UserHomeDirFunc: func() (string, error) { return t.TempDir(), nil },
			}

			err := runDaemonEnsure(t.Context(), log.NewLogger(), osProxy, utils.DefaultDecoderFactory{}, map[string]string{}, tc.pushChanged)
			require.NoError(t, err)

			assert.Equal(t, tc.pushChanged, gotPush)
			// Xcode activate wires exactly the xcelerate-proxy service.
			require.Len(t, gotServices, 1)
			assert.Equal(t, daemonpkg.ServiceXcelerateProxy, gotServices[0].Name)
		})
	}
}

func TestRunDaemonEnsure_forwardsSkipEnvVarViaEnvs(t *testing.T) {
	var gotEnvs map[string]string
	swapEnsureFn(t, func(_ context.Context, _ log.Logger, _ []daemonpkg.Service, _ bool, deps daemonpkg.EnsureDeps) error {
		gotEnvs = deps.Envs

		return nil
	})

	osProxy := &utilsMocks.OsProxyMock{
		OpenFileFunc: func(string, int, os.FileMode) (*os.File, error) { return nil, os.ErrNotExist },
		UserHomeDirFunc: func() (string, error) { return t.TempDir(), nil },
	}
	envs := map[string]string{daemonpkg.EnvSkipEnsure: "1"}

	err := runDaemonEnsure(t.Context(), log.NewLogger(), osProxy, utils.DefaultDecoderFactory{}, envs, false)
	require.NoError(t, err)
	assert.Equal(t, "1", gotEnvs[daemonpkg.EnvSkipEnsure])
}

func TestXcelerateRunDaemonEnsure_propagatesEnsureError(t *testing.T) {
	swapEnsureFn(t, func(context.Context, log.Logger, []daemonpkg.Service, bool, daemonpkg.EnsureDeps) error {
		return errors.New("launchctl bootstrap: permission denied")
	})

	osProxy := &utilsMocks.OsProxyMock{
		OpenFileFunc: func(string, int, os.FileMode) (*os.File, error) { return nil, os.ErrNotExist },
		UserHomeDirFunc: func() (string, error) { return t.TempDir(), nil },
	}

	err := runDaemonEnsure(t.Context(), log.NewLogger(), osProxy, utils.DefaultDecoderFactory{}, map[string]string{}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}
