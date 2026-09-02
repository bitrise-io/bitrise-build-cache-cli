//go:build unit

package reactnative

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// The React Native runner starts the ccache storage helper for RN builds. It
// must do so through the shared internal/ccache socket, whose Start goes to
// internal/spawn — not through a spawn of its own. A local copy elsewhere in
// this repo forgot to detach, and the helper died with the shell that made it.
func TestNewRunner_WiresTheSharedCcacheSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := ccacheconfig.ConfigFile(utils.DefaultOsProxy{})
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath,
		[]byte(`{"ipc_endpoint":"`+filepath.Join(home, "ipc.sock")+`"}`), 0o600))

	r := NewRunner(RunnerParams{})

	require.NotNil(t, r.socket, "a readable ccache config must yield a socket")
	assert.IsType(t, &ccacheipc.Socket{}, r.socket,
		"RN must reach the helper through internal/ccache.Socket")
}
