//nolint:maintidx
package gradleconfig

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	commonmocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func minimalJWT(t *testing.T, workspaceID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"authorization": map[string]any{
			"permissions": []map[string]any{
				{"rsname": "default", "claims": map[string]any{"org_id": []string{workspaceID}}},
			},
		},
	})
	require.NoError(t, err)

	return "hdr." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func Test_activateGradleParams(t *testing.T) {
	prep := func() log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Warnf", mock.Anything).Return()
		mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	tests := []struct {
		name    string
		debug   bool
		params  ActivateGradleParams
		envVars map[string]string
		want    TemplateInventory
		wantErr string
	}{
		{
			name: "no auth token",
			params: ActivateGradleParams{
				Cache:      CacheParams{Enabled: false},
				Analytics:  AnalyticsParams{Enabled: false},
				TestDistro: TestDistroParams{Enabled: false},
			},
			envVars: map[string]string{},
			wantErr: fmt.Errorf(ErrFmtReadAuthConfig, auth.ErrTokenNotProvided).Error(),
		},
		{
			name: "no workspaceID",
			params: ActivateGradleParams{
				Cache:      CacheParams{Enabled: false},
				Analytics:  AnalyticsParams{Enabled: false},
				TestDistro: TestDistroParams{Enabled: false},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN": "AuthTokenValue",
			},
			wantErr: fmt.Errorf(ErrFmtReadAuthConfig, auth.ErrWorkspaceIDNotProvided).Error(),
		},
		{
			name: "no plugins",
			params: ActivateGradleParams{
				Cache:      CacheParams{Enabled: false},
				Analytics:  AnalyticsParams{Enabled: false},
				TestDistro: TestDistroParams{Enabled: false},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
			},
			want: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:             "WorkspaceIDValue:AuthTokenValue",
					Version:               consts.GradleCommonPluginDepVersion,
					CLIPath:               "bitrise-build-cache",
					ProjectMarkerFilename: paths.ProjectMarkerFilename,
				},
				Cache: CacheTemplateInventory{
					Usage: UsageLevelNone,
				},
				Analytics: AnalyticsTemplateInventory{
					Usage: UsageLevelNone,
				},
				TestDistro: TestDistroTemplateInventory{
					Usage: UsageLevelNone,
				},
			},
		},
		{
			name: "dependency only plugins",
			params: ActivateGradleParams{
				Cache: CacheParams{
					Enabled:        false,
					JustDependency: true,
				},
				Analytics: AnalyticsParams{
					Enabled:        false,
					JustDependency: true,
				},
				TestDistro: TestDistroParams{
					Enabled:        false,
					JustDependency: true,
				},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
			},
			want: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:             "WorkspaceIDValue:AuthTokenValue",
					Version:               consts.GradleCommonPluginDepVersion,
					CLIPath:               "bitrise-build-cache",
					ProjectMarkerFilename: paths.ProjectMarkerFilename,
				},
				Cache: CacheTemplateInventory{
					Usage:   UsageLevelDependency,
					Version: consts.GradleRemoteBuildCachePluginDepVersion,
				},
				Analytics: AnalyticsTemplateInventory{
					Usage:   UsageLevelDependency,
					Version: consts.GradleAnalyticsPluginDepVersion,
				},
				TestDistro: TestDistroTemplateInventory{
					Usage:   UsageLevelDependency,
					Version: consts.GradleTestDistributionPluginDepVersion,
				},
			},
		},
		{
			name: "activate cache",
			params: ActivateGradleParams{
				Cache: CacheParams{
					Enabled:         true,
					JustDependency:  true, // gets overridden by enable
					ValidationLevel: string(CacheValidationLevelError),
					Endpoint:        "EndpointValue",
					PushEnabled:     true,
				},
				Analytics: AnalyticsParams{
					Enabled: false,
				},
				TestDistro: TestDistroParams{
					Enabled: false,
				},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
			},
			want: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:             "WorkspaceIDValue:AuthTokenValue",
					Version:               consts.GradleCommonPluginDepVersion,
					CLIPath:               "bitrise-build-cache",
					ProjectMarkerFilename: paths.ProjectMarkerFilename,
				},
				Cache: CacheTemplateInventory{
					Usage:               UsageLevelEnabled,
					Version:             consts.GradleRemoteBuildCachePluginDepVersion,
					EndpointURLWithPort: "EndpointValue",
					IsPushEnabled:       true,
					ValidationLevel:     string(CacheValidationLevelError),
				},
				Analytics: AnalyticsTemplateInventory{
					Usage: UsageLevelNone,
				},
				TestDistro: TestDistroTemplateInventory{
					Usage: UsageLevelNone,
				},
			},
		},
		{
			name: "cache-push implies cache-enabled",
			params: ActivateGradleParams{
				Cache: CacheParams{
					Enabled:         false,
					PushEnabled:     true,
					ValidationLevel: string(CacheValidationLevelWarning),
					Endpoint:        "EndpointValue",
				},
				Analytics: AnalyticsParams{
					Enabled: false,
				},
				TestDistro: TestDistroParams{
					Enabled: false,
				},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
			},
			want: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:             "WorkspaceIDValue:AuthTokenValue",
					Version:               consts.GradleCommonPluginDepVersion,
					CLIPath:               "bitrise-build-cache",
					ProjectMarkerFilename: paths.ProjectMarkerFilename,
				},
				Cache: CacheTemplateInventory{
					Usage:               UsageLevelEnabled,
					Version:             consts.GradleRemoteBuildCachePluginDepVersion,
					EndpointURLWithPort: "EndpointValue",
					IsPushEnabled:       true,
					ValidationLevel:     string(CacheValidationLevelWarning),
				},
				Analytics: AnalyticsTemplateInventory{
					Usage: UsageLevelNone,
				},
				TestDistro: TestDistroTemplateInventory{
					Usage: UsageLevelNone,
				},
			},
		},
		{
			name: "given invalid cache validation level cache activation throws error",
			params: ActivateGradleParams{
				Cache: CacheParams{
					Enabled:         true,
					ValidationLevel: "InvalidLevel",
					Endpoint:        "EndpointValue",
					PushEnabled:     true,
				},
				Analytics: AnalyticsParams{
					Enabled: false,
				},
				TestDistro: TestDistroParams{
					Enabled: false,
				},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
			},
			wantErr: fmt.Errorf(errFmtCacheConfigCreation, errors.New(errFmtInvalidCacheLevel)).Error(),
		},
		{
			name: "activate analytics",
			params: ActivateGradleParams{
				Cache: CacheParams{
					Enabled: false,
				},
				Analytics: AnalyticsParams{
					Enabled:        true,
					JustDependency: true, // gets overridden by enable
				},
				TestDistro: TestDistroParams{
					Enabled: false,
				},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
			},
			want: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:             "WorkspaceIDValue:AuthTokenValue",
					Version:               consts.GradleCommonPluginDepVersion,
					CLIPath:               "bitrise-build-cache",
					ProjectMarkerFilename: paths.ProjectMarkerFilename,
				},
				Cache: CacheTemplateInventory{
					Usage: UsageLevelNone,
				},
				Analytics: AnalyticsTemplateInventory{
					Usage:        UsageLevelEnabled,
					Version:      consts.GradleAnalyticsPluginDepVersion,
					Endpoint:     consts.GradleAnalyticsEndpoint,
					Port:         consts.GradleAnalyticsPort,
					HTTPEndpoint: consts.GradleAnalyticsHTTPEndpoint,
					GRPCEndpoint: consts.GradleAnalyticsGRPCEndpoint,
				},
				TestDistro: TestDistroTemplateInventory{
					Usage: UsageLevelNone,
				},
			},
		},
		{
			name: "activate test distro",
			params: ActivateGradleParams{
				Cache: CacheParams{
					Enabled: false,
				},
				Analytics: AnalyticsParams{
					Enabled: false,
				},
				TestDistro: TestDistroParams{
					Enabled:        true,
					JustDependency: true, // gets overridden by enable
					ShardSize:      25,
				},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
				"BITRISE_IO":                       "true",
				"BITRISE_BUILD_SLUG":               "BuildSlugValue",
				"BITRISE_APP_SLUG":                 "AppSlugValue",
			},
			want: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:             "WorkspaceIDValue:AuthTokenValue",
					AppSlug:               "AppSlugValue",
					CIProvider:            "bitrise",
					Version:               consts.GradleCommonPluginDepVersion,
					CLIPath:               "bitrise-build-cache",
					ProjectMarkerFilename: paths.ProjectMarkerFilename,
				},
				Cache: CacheTemplateInventory{
					Usage: UsageLevelNone,
				},
				Analytics: AnalyticsTemplateInventory{
					Usage: UsageLevelNone,
				},
				TestDistro: TestDistroTemplateInventory{
					Usage:      UsageLevelEnabled,
					Version:    consts.GradleTestDistributionPluginDepVersion,
					Endpoint:   consts.GradleTestDistributionEndpoint,
					KvEndpoint: consts.GradleTestDistributionKvEndpoint,
					Port:       consts.GradleTestDistributionPort,
					LogLevel:   "warning",
					ShardSize:  25,
				},
			},
		},
		{
			name:  "activate plugins with debug mode",
			debug: true,
			params: ActivateGradleParams{
				Cache: CacheParams{
					Enabled: false,
				},
				Analytics: AnalyticsParams{
					Enabled: false,
				},
				TestDistro: TestDistroParams{
					Enabled:        true,
					JustDependency: true, // gets overridden by enable
				},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
				"BITRISE_IO":                       "true",
				"BITRISE_BUILD_SLUG":               "BuildSlugValue",
				"BITRISE_APP_SLUG":                 "AppSlugValue",
			},
			want: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:             "WorkspaceIDValue:AuthTokenValue",
					Debug:                 true,
					AppSlug:               "AppSlugValue",
					CIProvider:            "bitrise",
					Version:               consts.GradleCommonPluginDepVersion,
					CLIPath:               "bitrise-build-cache",
					ProjectMarkerFilename: paths.ProjectMarkerFilename,
				},
				Cache: CacheTemplateInventory{
					Usage: UsageLevelNone,
				},
				Analytics: AnalyticsTemplateInventory{
					Usage: UsageLevelNone,
				},
				TestDistro: TestDistroTemplateInventory{
					Usage:      UsageLevelEnabled,
					Version:    consts.GradleTestDistributionPluginDepVersion,
					Endpoint:   consts.GradleTestDistributionEndpoint,
					KvEndpoint: consts.GradleTestDistributionKvEndpoint,
					Port:       consts.GradleTestDistributionPort,
					LogLevel:   "debug",
				},
			},
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			mockLogger := prep()
			got, err := tt.params.TemplateInventory(mockLogger, tt.envVars, tt.debug, nil)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

// A JWT-only auth resolution (no stored PAT / no BITRISE_BUILD_CACHE_AUTH_TOKEN)
// must blank Common.AuthToken so the template skips baking the JWT into the init
// script — leakage that scripts/check_gradle_jwt_auth.sh grep-fails on.
func Test_TemplateInventory_JWTOnly_BlanksBakedAuthToken(t *testing.T) {
	logger := &mocks.Logger{}
	logger.On("Infof", mock.Anything).Return()
	logger.On("Infof", mock.Anything, mock.Anything).Return()
	logger.On("Debugf", mock.Anything).Return()
	logger.On("Debugf", mock.Anything, mock.Anything).Return()
	logger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	logger.On("Errorf", mock.Anything).Return()
	logger.On("Errorf", mock.Anything, mock.Anything).Return()
	logger.On("Warnf", mock.Anything).Return()
	logger.On("Warnf", mock.Anything, mock.Anything).Return()

	envs := map[string]string{
		"BITRISE_IO":         "true",
		"BITRISE_BUILD_SLUG": "build-slug",
		"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": minimalJWT(t, "jwt-ws"),
	}

	params := DefaultActivateGradleParams()
	params.Cache.Enabled = true
	params.Cache.PushEnabled = true

	inv, err := params.TemplateInventory(logger, envs, false, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, inv.Common.CIProvider, "CI-context guard: this test asserts the CI-side gating")
	assert.Empty(t, inv.Common.AuthToken, "JWT-only auth must not surface a bakeable token")

	got, err := inv.GenerateInitGradle(GradleTemplateProxy())
	require.NoError(t, err)
	assert.NotContains(t, got, "authToken", "the check script grep-fails on any authToken occurrence in the init script")
}

func Test_TemplateInventory_WorkspacesOnly_BlanksBakedAuthToken(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, store.NewKeychain().Save(auth.TokenSet{
		Workspaces: map[string]auth.TokenSet{
			"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
		},
	}))

	logger := &mocks.Logger{}
	logger.On("Infof", mock.Anything).Return()
	logger.On("Infof", mock.Anything, mock.Anything).Return()
	logger.On("Debugf", mock.Anything).Return()
	logger.On("Debugf", mock.Anything, mock.Anything).Return()
	logger.On("Errorf", mock.Anything).Return()
	logger.On("Errorf", mock.Anything, mock.Anything).Return()
	logger.On("Warnf", mock.Anything).Return()
	logger.On("Warnf", mock.Anything, mock.Anything).Return()

	params := DefaultActivateGradleParams()
	params.Cache.Enabled = true
	params.Cache.PushEnabled = true

	inv, err := params.TemplateInventory(logger, map[string]string{}, false, nil)
	require.NoError(t, err, "scenario B must not fail the resolve")
	assert.Empty(t, inv.Common.AuthToken, "workspaces-only auth must not surface a bakeable token")

	got, err := inv.GenerateInitGradle(GradleTemplateProxy())
	require.NoError(t, err)
	assert.NotContains(t, got, "authToken=\"", "the ValueSource must resolve per build, not a baked literal")
}

func Test_TemplateInventory_BenchmarkPhase(t *testing.T) {
	prep := func() log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Warnf", mock.Anything).Return()
		mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	t.Run("benchmark provider is called on CI and baseline disables cache", func(t *testing.T) {
		logger := prep()
		envs := map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "auth-token",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
			"BITRISE_IO":                       "true",
			"BITRISE_BUILD_SLUG":               "build-slug",
			"BITRISE_APP_SLUG":                 "app-slug",
			"BITRISE_TRIGGERED_WORKFLOW_ID":    "primary",
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(buildTool string, _ common.CacheConfigMetadata) (string, error) {
				assert.Equal(t, common.BuildToolGradle, buildTool)

				return common.BenchmarkPhaseBaseline, nil
			},
		}

		params := ActivateGradleParams{
			Cache:     CacheParams{Enabled: true, PushEnabled: true},
			Analytics: AnalyticsParams{Enabled: false},
		}

		inv, err := params.TemplateInventory(logger, envs, false, mockProvider)
		require.NoError(t, err)

		assert.Len(t, mockProvider.GetBenchmarkPhaseCalls(), 1)
		// Baseline disables cache
		assert.Equal(t, UsageLevelNone, inv.Cache.Usage)
	})

	t.Run("benchmark provider is not called when CI provider is empty", func(t *testing.T) {
		logger := prep()
		envs := map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "auth-token",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return common.BenchmarkPhaseBaseline, nil
			},
		}

		params := DefaultActivateGradleParams()

		_, err := params.TemplateInventory(logger, envs, false, mockProvider)
		require.NoError(t, err)

		assert.Empty(t, mockProvider.GetBenchmarkPhaseCalls())
	})
}
