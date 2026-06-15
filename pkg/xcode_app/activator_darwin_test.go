//go:build unit && darwin

package xcode_app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
	xa "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcode_app"
)

// stubBackend implements daemon.Backend without touching the OS supervisor.
// Records Install/Start calls so tests can assert the activator wired the
// xcelerate-proxy service through correctly.
type stubBackend struct {
	installed []daemon.Service
	started   []daemon.Service
}

// Name returns "launchd" so daemon.Up's configPath dispatcher uses
// PlistPath — the test setup writes the supervisor config under that exact
// path so the Stat-then-Start check inside daemon.Up succeeds.
func (*stubBackend) Name() string { return "launchd" }

func (b *stubBackend) Install(_ context.Context, paths daemon.Paths, svc daemon.Service, _ string) (string, error) {
	b.installed = append(b.installed, svc)

	// daemon.Up does Stat-then-Start on the plist path; write a placeholder
	// so the upstream Stat succeeds. The contents don't matter — the stub
	// Start ignores them.
	path := paths.PlistPath(svc.Label())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil { //nolint:gosec // test plist must be readable
		return "", err
	}

	return path, nil
}

func (b *stubBackend) Uninstall(_ context.Context, paths daemon.Paths, svc daemon.Service) (string, bool, error) {
	return paths.PlistPath(svc.Label()), true, nil
}

func (b *stubBackend) Start(_ context.Context, _ daemon.Paths, svc daemon.Service) error {
	b.started = append(b.started, svc)

	return nil
}

func (*stubBackend) Stop(_ context.Context, _ daemon.Paths, _ daemon.Service) error { return nil }

// recordingRunner is a daemon.CommandRunner that captures the args of every
// invocation so tests can assert on the launchctl calls the activator made.
type recordingRunner struct {
	calls [][]string
}

func (r *recordingRunner) Run(_ context.Context, bin string, args ...string) (string, string, int, error) {
	r.calls = append(r.calls, append([]string{bin}, args...))

	return "", "", 0, nil
}

// stubXcodeChecker reports no running Xcode processes; tests that care about
// the relaunch nudge construct a different one inline.
type stubXcodeChecker struct{ pids []int }

func (s stubXcodeChecker) RunningPIDs(_ context.Context) ([]int, error) {
	return s.pids, nil
}

// setupDarwinFixture wires t.TempDir() as $HOME and seeds a valid xcelerate
// config so ReadConfig succeeds.
func setupDarwinFixture(t *testing.T, proxySocket string) (home string) {
	t.Helper()

	home = t.TempDir()
	t.Setenv("HOME", home)

	xceleratehome := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(xceleratehome, 0o755))

	cfg := map[string]any{
		"proxyVersion":           "test",
		"proxySocketPath":        proxySocket,
		"cliVersion":             "test",
		"wrapperVersion":         "test",
		"originalXcodebuildPath": "/usr/bin/xcodebuild",
		"originalXcrunPath":      "/usr/bin/xcrun",
		"buildCacheEnabled":      true,
		"buildCacheSkipFlags":    false,
		"buildCacheEndpoint":     "",
		"pushEnabled":            true,
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(xceleratehome, "config.json"), raw, 0o644))

	return home
}

func newActivatorForTest(t *testing.T, home string, envs map[string]string) (*Activator, *recordingRunner, *stubBackend) {
	t.Helper()

	runner := &recordingRunner{}
	backend := &stubBackend{}
	paths := daemon.NewPathsFromHome(home)

	a := &Activator{
		Logger:        newLogger(),
		Envs:          envs,
		Launchctl:     xa.LaunchctlClient{Runner: runner, Bin: "/fake/launchctl"},
		XcodeChecker:  stubXcodeChecker{},
		DaemonBackend: backend,
		DaemonPaths:   &paths,
		Executable:    "/fake/cli",
	}

	return a, runner, backend
}

func TestEnable_writesXCConfigAndStateFile_noPrevious(t *testing.T) {
	home := setupDarwinFixture(t, "/tmp/xcelerate-proxy.sock")
	a, runner, backend := newActivatorForTest(t, home, map[string]string{})

	got, err := a.Enable(context.Background())
	require.NoError(t, err)

	xcconfigPath := filepath.Join(home, ".bitrise-xcelerate", xa.OverrideXCConfigFileName)
	assert.Equal(t, xcconfigPath, got.XCConfigPath)

	content, err := os.ReadFile(xcconfigPath) //nolint:gosec
	require.NoError(t, err)
	assert.Contains(t, string(content), "COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock")
	assert.NotContains(t, string(content), "#include")

	statePath := filepath.Join(home, ".bitrise-xcelerate", xa.StateFileName)
	state, found, err := xa.LoadState(statePath)
	require.NoError(t, err)
	require.True(t, found)
	assert.Empty(t, state.PreviousXCConfigPath, "no previous override at enable time")

	assertLaunchctlCall(t, runner, "setenv", xa.XCConfigEnvVar, xcconfigPath)

	require.Len(t, backend.installed, 1)
	assert.Equal(t, "xcelerate-proxy", backend.installed[0].Name)
	require.Len(t, backend.started, 1)
	assert.Equal(t, "xcelerate-proxy", backend.started[0].Name)
}

func TestEnable_chainsPreviousXCConfigViaInclude(t *testing.T) {
	home := setupDarwinFixture(t, "/tmp/xcelerate-proxy.sock")
	previousPath := "/Users/me/Base.xcconfig"
	a, _, _ := newActivatorForTest(t, home, map[string]string{xa.XCConfigEnvVar: previousPath})

	_, err := a.Enable(context.Background())
	require.NoError(t, err)

	xcconfigPath := filepath.Join(home, ".bitrise-xcelerate", xa.OverrideXCConfigFileName)
	content, err := os.ReadFile(xcconfigPath) //nolint:gosec
	require.NoError(t, err)
	assert.Contains(t, string(content), `#include "/Users/me/Base.xcconfig"`)

	state, found, err := xa.LoadState(filepath.Join(home, ".bitrise-xcelerate", xa.StateFileName))
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, previousPath, state.PreviousXCConfigPath)
}

func TestEnable_selfLoopPreservesOriginalState(t *testing.T) {
	// Regression for the review's blocker (issue 1).
	//
	// First enable: previous == "/Users/me/Base.xcconfig" — user's real
	// prior override. Stored on disk.
	// Second enable: previous == our own override path (would happen if
	// the user re-runs enable from a shell launched in the GUI session
	// that already inherited our XCODE_XCCONFIG_FILE). Activator must
	// preserve "/Users/me/Base.xcconfig" in state — not clobber it with
	// our own path.
	home := setupDarwinFixture(t, "/tmp/xcelerate-proxy.sock")
	originalPath := "/Users/me/Base.xcconfig"

	// First enable with the real user-supplied prior override.
	a, _, _ := newActivatorForTest(t, home, map[string]string{xa.XCConfigEnvVar: originalPath})
	_, err := a.Enable(context.Background())
	require.NoError(t, err)

	xcconfigPath := filepath.Join(home, ".bitrise-xcelerate", xa.OverrideXCConfigFileName)

	// Second enable — env now points at our own xcconfig (the self-loop).
	a2, _, _ := newActivatorForTest(t, home, map[string]string{xa.XCConfigEnvVar: xcconfigPath})
	_, err = a2.Enable(context.Background())
	require.NoError(t, err)

	state, found, err := xa.LoadState(filepath.Join(home, ".bitrise-xcelerate", xa.StateFileName))
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, originalPath, state.PreviousXCConfigPath, "state must still point at the user's real prior override after the self-loop")

	// Override xcconfig must also keep the `#include` of the original.
	content, err := os.ReadFile(xcconfigPath) //nolint:gosec
	require.NoError(t, err)
	assert.Contains(t, string(content), `#include "`+originalPath+`"`)
}

func TestDisable_restoresPreviousXCConfig(t *testing.T) {
	home := setupDarwinFixture(t, "/tmp/xcelerate-proxy.sock")
	previousPath := "/Users/me/Base.xcconfig"
	a, runner, _ := newActivatorForTest(t, home, map[string]string{xa.XCConfigEnvVar: previousPath})

	_, err := a.Enable(context.Background())
	require.NoError(t, err)

	got, err := a.Disable(context.Background())
	require.NoError(t, err)

	assert.True(t, got.LaunchAgentRemoved)
	assert.True(t, got.XCConfigRemoved)
	assert.Equal(t, previousPath, got.RestoredXCConfigPath)

	// State file gone.
	_, found, err := xa.LoadState(filepath.Join(home, ".bitrise-xcelerate", xa.StateFileName))
	require.NoError(t, err)
	assert.False(t, found)

	// XCConfig override gone.
	_, statErr := os.Stat(filepath.Join(home, ".bitrise-xcelerate", xa.OverrideXCConfigFileName))
	require.Error(t, statErr)

	// Last launchctl call is setenv to the restored previous path.
	assertLaunchctlCall(t, runner, "setenv", xa.XCConfigEnvVar, previousPath)
}

func TestDisable_noPriorStateClearsEnv(t *testing.T) {
	home := setupDarwinFixture(t, "/tmp/xcelerate-proxy.sock")
	a, runner, _ := newActivatorForTest(t, home, map[string]string{})

	_, err := a.Enable(context.Background())
	require.NoError(t, err)

	got, err := a.Disable(context.Background())
	require.NoError(t, err)

	assert.Empty(t, got.RestoredXCConfigPath)
	assertLaunchctlCall(t, runner, "unsetenv", xa.XCConfigEnvVar)
}

// assertLaunchctlCall asserts the recorded runner saw at least one call
// whose args exactly match the supplied verb + value tuple (in order). Used
// to verify Activator sent the right launchctl invocations without caring
// about ordering between setenv and bootstrap.
func assertLaunchctlCall(t *testing.T, runner *recordingRunner, verbAndArgs ...string) {
	t.Helper()

	for _, call := range runner.calls {
		// Skip the launchctl bin position; compare from args[1:].
		if len(call) < 1+len(verbAndArgs) {
			continue
		}

		joined := strings.Join(call[1:1+len(verbAndArgs)], "\x00")
		want := strings.Join(verbAndArgs, "\x00")
		if joined == want {
			return
		}
	}

	t.Fatalf("expected a launchctl call with args %v; got %v", verbAndArgs, runner.calls)
}
