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

func TestNewActivator_CppRequiresGradle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cases := []struct {
		name   string
		params ActivatorParams
		wantCpp bool
	}{
		{
			name:    "cpp+gradle both on → cpp activated",
			params:  ActivatorParams{GradleEnabled: true, CppEnabled: true},
			wantCpp: true,
		},
		{
			name:    "cpp on, gradle off → cpp skipped",
			params:  ActivatorParams{GradleEnabled: false, CppEnabled: true},
			wantCpp: false,
		},
		{
			name:    "cpp off → cpp skipped regardless of gradle",
			params:  ActivatorParams{GradleEnabled: true, CppEnabled: false},
			wantCpp: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewActivator(tc.params)

			if tc.wantCpp {
				assert.NotNil(t, a.cpp, "cpp activator should be wired")
				assert.NotNil(t, a.helper, "ccache storage helper starter should be wired")
			} else {
				assert.Nil(t, a.cpp, "cpp activator should not be wired")
				assert.Nil(t, a.helper, "ccache storage helper starter should not be wired")
			}
		})
	}
}
