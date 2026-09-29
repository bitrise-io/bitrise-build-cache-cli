//go:build unit

package ccache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsmocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func isolate(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)

	return home
}

// populatedCIEnv is everything the monolith injects into a build. A warmup that
// runs inside one sees exactly this, and must persist none of it.
func populatedCIEnv() map[string]string {
	return map[string]string{
		"BITRISE_IO":         "true",
		"BITRISE_BUILD_SLUG": "build-slug-abcdef12",
		"BITRISE_APP_SLUG":   "app-slug-abcdef12",
		auth.EnvAuthToken:    "auth-token-abcdef12",
		auth.EnvWorkspaceID:  "workspace-id-abcdef12",
	}
}

// $TMPDIR is per-session on macOS, so warmup's socket is not the one the
// build's helper binds: CCACHE_REMOTE_STORAGE would point at a dead socket and
// every lookup would silently miss. The idle timeout likewise depends on
// whether the *build* is CI, which warmup cannot know.
func TestNewConfig_LitePersistsNoEnvironmentDerivedValues(t *testing.T) {
	isolate(t)
	envs := populatedCIEnv()

	osProxy := &utilsmocks.OsProxyMock{
		TempDirFunc:     func() string { return "/warmup-tmp" },
		UserHomeDirFunc: func() (string, error) { return t.TempDir(), nil },
		HostnameFunc:    func() (string, error) { return "ci-host", nil },
	}

	config, err := NewConfig(t.Context(), envs, osProxy, Params{Lite: true, PushEnabled: true})
	require.NoError(t, err)

	assert.Empty(t, config.IPCEndpoint, "warmup's $TMPDIR is not the build's")
	assert.Zero(t, config.IdleTimeout, "warmup cannot know whether the build is CI")
	assert.Empty(t, config.AuthConfig.Token)

	body, err := json.Marshal(config)
	require.NoError(t, err)
	for name, value := range envs {
		if len(value) < 8 {
			continue
		}
		assert.NotContains(t, string(body), value, "%s leaked into the config", name)
	}
}

// ...and the readers must still get usable values, resolved in their own
// environment rather than read out of the file.
func TestReadConfig_ResolvesAbsentEnvironmentDerivedValues(t *testing.T) {
	home := isolate(t)

	osProxy := &utilsmocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		OpenFileFunc:    utils.DefaultOsProxy{}.OpenFile,
		TempDirFunc:     func() string { return "/build-tmp" },
		HostnameFunc:    func() (string, error) { return "ci-host", nil },
	}

	dir := DirPath(osProxy)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"enabled":true}`), 0o600))

	envs := map[string]string{"BITRISE_IO": "true", "BITRISE_BUILD_SLUG": "b"}
	config, err := ReadConfig(osProxy, utils.DefaultDecoderFactory{}, envs)
	require.NoError(t, err)

	assert.Equal(t, ResolveIPCSocketPath("", envs, osProxy), config.IPCEndpoint)
	assert.NotEmpty(t, config.IPCEndpoint)
	assert.Equal(t, ciIdleTimeout, config.IdleTimeout, "the build's CI-ness, not warmup's")
}
