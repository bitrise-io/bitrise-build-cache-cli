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
	opts   []ccacheipc.StartOption
	socket string
}

// The real starter re-execs this binary, which under `go test` is the test
// binary — it would re-run the suite recursively.
func fakeHelperStart(t *testing.T) *helperStart {
	t.Helper()

	rec := &helperStart{}
	prevStart, prevBudget := startHelperFn, helperReadyBudget
	startHelperFn = func(socketPath string, opts ...ccacheipc.StartOption) error {
		rec.calls++
		rec.socket = socketPath
		rec.opts = opts

		return nil
	}
	helperReadyBudget = 50 * time.Millisecond
	t.Cleanup(func() { startHelperFn, helperReadyBudget = prevStart, prevBudget })

	return rec
}

// Starting the helper used to live in cmd/, so pkg/ consumers — the wizard
// among them — activated ccache and left nothing serving the socket. ccache
// then misses every lookup for the whole build, silently.
func TestEnsureHelperServing_StartsTheHelperWhenTheSocketIsDead(t *testing.T) {
	rec := fakeHelperStart(t)
	socketPath := filepath.Join(shortTempDir(t), "ipc.sock")

	a := NewActivator(ActivatorParams{Logger: log.NewLogger(), Envs: map[string]string{}})
	a.ensureHelperServing(context.Background(), socketPath)

	require.Equal(t, 1, rec.calls, "activation must start the helper")
	assert.Equal(t, socketPath, rec.socket, "and on the socket the config records")
}

// The invocation ID ties the helper's analytics to this build; the detached
// helper starts in a shell that never saw the env var.
func TestEnsureHelperServing_ForwardsTheInvocationID(t *testing.T) {
	rec := fakeHelperStart(t)

	a := NewActivator(ActivatorParams{
		Logger: log.NewLogger(),
		Envs:   map[string]string{"BITRISE_INVOCATION_ID": "inv-1"},
	})
	a.ensureHelperServing(context.Background(), filepath.Join(shortTempDir(t), "ipc.sock"))

	require.Equal(t, 1, rec.calls)
	assert.Len(t, rec.opts, 1, "the invocation ID must reach the helper")
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
	assert.Len(t, rec.opts, 1, "--debug must reach the supervised-free helper")
}

// The case that bites: a crashed helper leaves its socket file behind, so a
// stat-only check would call it healthy and every ccache lookup would miss for
// the whole build. The handshake is what distinguishes the two.
func TestEnsureHelperServing_StartsOverAStaleSocketFile(t *testing.T) {
	rec := fakeHelperStart(t)
	socketPath := filepath.Join(shortTempDir(t), "stale.sock")
	require.NoError(t, os.WriteFile(socketPath, nil, 0o600))

	a := NewActivator(ActivatorParams{Logger: log.NewLogger(), Envs: map[string]string{}})
	a.ensureHelperServing(context.Background(), socketPath)

	assert.Equal(t, 1, rec.calls, "a leftover socket file must not pass as a serving helper")
}

// Activation is where a user migrates off a supervised helper, so it has to
// retire a launch agent an older CLI left behind.
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
