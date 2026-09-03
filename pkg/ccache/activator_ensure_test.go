//go:build unit

package ccache

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

func shortTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "ac")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return dir
}

type helperStart struct {
	calls  int
	args   []string
	socket string
}

// The real starter re-execs the test binary and would rerun the suite.
func fakeHelperStart(t *testing.T) *helperStart {
	t.Helper()

	rec := &helperStart{}
	prevStart, prevBudget := startHelperFn, helperReadyBudget
	startHelperFn = func(socketPath string, opts ...ccacheipc.StartOption) error {
		rec.calls++
		rec.socket = socketPath
		// Resolve the options to the argv they produce, so an assertion cannot
		// pass on the wrong option merely because the count matches.
		rec.args = ccacheipc.HelperArgs(opts...)

		return nil
	}
	helperReadyBudget = 50 * time.Millisecond
	t.Cleanup(func() { startHelperFn, helperReadyBudget = prevStart, prevBudget })

	return rec
}

// A pkg/ consumer that activates ccache and leaves nothing serving the socket
// makes ccache miss every lookup for the whole build, silently.
func TestEnsureHelperServing_StartsTheHelperWhenTheSocketIsDead(t *testing.T) {
	rec := fakeHelperStart(t)
	socketPath := filepath.Join(shortTempDir(t), "ipc.sock")

	a := NewActivator(ActivatorParams{Logger: log.NewLogger(), Envs: map[string]string{}})
	a.ensureHelperServing(context.Background(), socketPath)

	require.Equal(t, 1, rec.calls, "activation must start the helper")
	assert.Equal(t, socketPath, rec.socket, "and on the socket the config records")
}

// The detached helper starts in a shell that never saw the env var.
func TestEnsureHelperServing_ForwardsTheInvocationID(t *testing.T) {
	rec := fakeHelperStart(t)

	a := NewActivator(ActivatorParams{
		Logger: log.NewLogger(),
		Envs:   map[string]string{"BITRISE_INVOCATION_ID": "inv-1"},
	})
	a.ensureHelperServing(context.Background(), filepath.Join(shortTempDir(t), "ipc.sock"))

	require.Equal(t, 1, rec.calls)
	assert.Equal(t,
		[]string{"ccache", "storage-helper", "start", "--invocation-id=inv-1"},
		rec.args)
}

func TestEnsureHelperServing_DebugLoggingReachesTheHelper(t *testing.T) {
	rec := fakeHelperStart(t)

	a := NewActivator(ActivatorParams{
		Logger:       log.NewLogger(),
		Envs:         map[string]string{},
		DebugLogging: true,
	})
	a.ensureHelperServing(context.Background(), filepath.Join(shortTempDir(t), "ipc.sock"))

	require.Equal(t, 1, rec.calls)
	assert.Equal(t,
		[]string{"--debug", "ccache", "storage-helper", "start"},
		rec.args,
		"--debug is a persistent root flag, so it has to precede the subcommand")
}

// A crashed helper leaves its socket file behind, so only the handshake tells
// it from a live one.
func TestEnsureHelperServing_StartsOverAStaleSocketFile(t *testing.T) {
	rec := fakeHelperStart(t)
	socketPath := filepath.Join(shortTempDir(t), "stale.sock")
	require.NoError(t, os.WriteFile(socketPath, nil, 0o600))

	a := NewActivator(ActivatorParams{Logger: log.NewLogger(), Envs: map[string]string{}})
	a.ensureHelperServing(context.Background(), socketPath)

	assert.Equal(t, 1, rec.calls, "a leftover socket file must not pass as a serving helper")
}

// Activation is where a user migrates off a supervised helper.
func TestEnsureHelperServing_RetiresALegacyLaunchAgent(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd-only: the linux arm shells out to systemctl")
	}

	fakeHelperStart(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.LaunchAgentsDir(), 0o755))
	plist := p.PlistPath("io.bitrise.build-cache." + spawn.NameCcacheHelper)
	require.NoError(t, os.WriteFile(plist, []byte("<plist/>"), 0o644))

	a := NewActivator(ActivatorParams{Logger: log.NewLogger(), Envs: map[string]string{}})
	a.ensureHelperServing(context.Background(), filepath.Join(shortTempDir(t), "ipc.sock"))

	assert.NoFileExists(t, plist, "activation must retire a leftover launch agent")
}
