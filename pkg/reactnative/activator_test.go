//go:build unit

package reactnative

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

func TestActivator_saveReactNativeMarker_WritesExpectedFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "token")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "workspace")

	a := NewActivator(ActivatorParams{
		GradleEnabled: true,
		XcodeEnabled:  true,
		CppEnabled:    false,
	})

	require.NoError(t, a.saveReactNativeMarker())

	cfg, err := rnconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.NoError(t, err)

	assert.True(t, cfg.Enabled)
	assert.True(t, cfg.Gradle)
	assert.True(t, cfg.Xcode)
	assert.False(t, cfg.Cpp)
	assert.Equal(t, "token", cfg.AuthConfig.AuthToken)
	assert.Equal(t, "workspace", cfg.AuthConfig.WorkspaceID)
	assert.False(t, cfg.ActivatedAt.IsZero())

	// File at the documented path.
	assert.FileExists(t, filepath.Join(home, ".bitrise/cache/reactnative/config.json"))
}
