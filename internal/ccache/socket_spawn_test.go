//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

func captureSpawn(t *testing.T) *spawn.Service {
	t.Helper()

	var got spawn.Service
	prev := detach
	detach = func(svc spawn.Service) (int, error) {
		got = svc

		return 4242, nil
	}
	t.Cleanup(func() { detach = prev })

	return &got
}

// Socket.Start is the funnel every ccache caller reaches, so its argv has to be
// the one internal/spawn defines rather than a local copy.
func TestSocketStart_SpawnsTheServiceSpawnDefines(t *testing.T) {
	got := captureSpawn(t)

	require.NoError(t, NewSocket("/tmp/x.sock").Start())

	assert.Equal(t, spawn.CcacheHelper().Args, got.Args)
	assert.Equal(t, spawn.NameCcacheHelper, got.Name)
}

// --debug is a persistent root flag, so cobra rejects it after the subcommand.
func TestSocketStart_PutsDebugBeforeTheSubcommand(t *testing.T) {
	got := captureSpawn(t)

	require.NoError(t, NewSocket("/tmp/x.sock").Start(WithDebug()))

	assert.Equal(t, []string{"--debug", "ccache", "storage-helper", "start"}, got.Args)
}

func TestSocketStart_AppendsTheInvocationID(t *testing.T) {
	got := captureSpawn(t)

	require.NoError(t, NewSocket("/tmp/x.sock").Start(WithInvocationID("abc123")))

	assert.Equal(t,
		[]string{"ccache", "storage-helper", "start", "--invocation-id=abc123"},
		got.Args)
}
