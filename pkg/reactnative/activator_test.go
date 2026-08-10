//go:build unit

package reactnative

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestActivator_SaveMarkers_WritesMarkerAndMultiplatformConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "bitpat_token")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "ws-1")

	a := NewActivator(ActivatorParams{Logger: log.NewLogger(), DebugLogging: true})
	require.NoError(t, a.SaveMarkers(t.Context()))

	rnCfg, err := rnconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.NoError(t, err)
	assert.True(t, rnCfg.Enabled)
	assert.FileExists(t, filepath.Join(home, ".bitrise/cache/reactnative/config.json"))

	mpCfg, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.NoError(t, err)
	assert.Equal(t, "bitpat_token", mpCfg.AuthConfig.AuthToken)
	assert.Equal(t, "ws-1", mpCfg.AuthConfig.WorkspaceID)
	assert.True(t, mpCfg.DebugLogging)
}

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
	t.Run("CI detected → workdir exported as $HOME/build", func(t *testing.T) {
		resetEASEnvAfter(t)
		t.Setenv("HOME", "/Users/vagrant")
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
		t.Setenv("HOME", "/Users/vagrant")
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
		name    string
		params  ActivatorParams
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

// Config.Save rewrites the whole file, so a fresh Config here erases the login.
func TestSaveMultiplatformConfig_KeepsExistingCredentials(t *testing.T) {
	// Pinning an env credential writes to a store; without this it reaches the
	// real OS keychain and blocks on the unlock prompt.
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "bitpat_refreshed")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "ws-1")

	login := auth.TokenSet{
		AuthToken:    "bitpat_minted",
		WorkspaceID:  "ws-1",
		RefreshToken: "refresh-me",
		Username:     "dev",
	}
	before := multiplatformconfig.Config{Credentials: &login}
	require.NoError(t, before.Save(utils.DefaultOsProxy{}, utils.DefaultEncoderFactory{}))

	require.NoError(t, saveMultiplatformConfig(t.Context(), utils.AllEnvs(), true))

	after, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.NoError(t, err)
	require.NotNil(t, after.Credentials, "the credentials block must survive activation")
	assert.Equal(t, "refresh-me", after.Credentials.RefreshToken)
	assert.Equal(t, "dev", after.Credentials.Username)
	assert.Equal(t, "bitpat_refreshed", after.AuthConfig.AuthToken, "AuthConfig still tracks the resolved token")
}
