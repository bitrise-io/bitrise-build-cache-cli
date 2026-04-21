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

func TestActivator_saveReactNativeMarker_WritesEnabledTrue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := NewActivator(ActivatorParams{
		GradleEnabled: true,
		XcodeEnabled:  true,
	})

	require.NoError(t, a.saveReactNativeMarker())

	cfg, err := rnconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.NoError(t, err)
	assert.True(t, cfg.Enabled)

	assert.FileExists(t, filepath.Join(home, ".bitrise/cache/reactnative/config.json"))
}
