//go:build unit

package reactnative

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
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

// resetEASEnvAfter clears any leftover EAS_LOCAL_BUILD_WORKINGDIR and any CI
// detection envs after the test, so that os-level state from one subtest does
// not bleed into another. t.Setenv handles the original values but doesn't
// clear vars set via os.Setenv inside the code under test.
func resetEASEnvAfter(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		os.Unsetenv(EASWorkingDirEnv)
	})
}

func TestActivator_exportEASWorkingDirIfCI(t *testing.T) {
	t.Run("Bitrise CI → workdir exported", func(t *testing.T) {
		resetEASEnvAfter(t)
		t.Setenv("BITRISE_IO", "true")
		t.Setenv("BITRISE_BUILD_SLUG", "abc")
		t.Setenv("CIRCLECI", "")
		t.Setenv("GITHUB_ACTIONS", "")
		t.Setenv("GITLAB_CI", "")
		t.Setenv(EASWorkingDirEnv, "")

		a := &Activator{logger: log.NewLogger()}
		a.exportEASWorkingDirIfCI()

		assert.Equal(t, "/Users/vagrant/build", os.Getenv(EASWorkingDirEnv))
	})

	t.Run("no CI detected → workdir NOT exported", func(t *testing.T) {
		resetEASEnvAfter(t)
		t.Setenv("BITRISE_IO", "")
		t.Setenv("BITRISE_BUILD_SLUG", "")
		t.Setenv("CIRCLECI", "")
		t.Setenv("GITHUB_ACTIONS", "")
		t.Setenv("GITLAB_CI", "")
		t.Setenv(EASWorkingDirEnv, "")

		a := &Activator{logger: log.NewLogger()}
		a.exportEASWorkingDirIfCI()

		assert.Empty(t, os.Getenv(EASWorkingDirEnv))
	})

	t.Run("user-supplied value preserved on CI", func(t *testing.T) {
		resetEASEnvAfter(t)
		t.Setenv("BITRISE_IO", "true")
		t.Setenv("BITRISE_BUILD_SLUG", "abc")
		t.Setenv(EASWorkingDirEnv, "/custom/path")

		a := &Activator{logger: log.NewLogger()}
		a.exportEASWorkingDirIfCI()

		assert.Equal(t, "/custom/path", os.Getenv(EASWorkingDirEnv))
	})
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
