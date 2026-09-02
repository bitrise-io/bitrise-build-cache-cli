//go:build unit

package ccache_test

import (
	"context"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
)

func TestDeactivator_DryRun_NoError(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	d := ccachepkg.NewDeactivator(ccachepkg.DeactivatorParams{
		DryRun: true,
		Logger: log.NewLogger(),
	})
	require.NoError(t, d.Deactivate(context.Background()))
}

// Deactivate is best-effort by design: an explicit socket override with nothing
// listening must not blow up.
func TestDeactivator_SocketOverride_Retained(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	d := ccachepkg.NewDeactivator(ccachepkg.DeactivatorParams{
		DryRun:             true,
		SocketPathOverride: "/tmp/does-not-exist.sock",
		Logger:             log.NewLogger(),
	})
	assert.NotNil(t, d)
	require.NoError(t, d.Deactivate(context.Background()))
}
