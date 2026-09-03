//go:build unit

package updater

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

// Missing either service leaves the user on the supervised path indefinitely.
func TestRemoveLegacySupervision_RetiresBothServices(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd-only: the linux arm shells out to systemctl")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.LaunchAgentsDir(), 0o755))

	planted := map[string]string{}
	for _, name := range []string{spawn.NameXcelerateProxy, spawn.NameCcacheHelper} {
		path := filepath.Join(p.LaunchAgentsDir(), "io.bitrise.build-cache."+name+".plist")
		require.NoError(t, os.WriteFile(path, []byte("<plist/>"), 0o644))
		planted[name] = path
	}

	removeLegacySupervision(context.Background(), log.NewLogger())

	for name, path := range planted {
		assert.NoFileExists(t, path, "an upgrade must retire the %s registration", name)
	}
}
