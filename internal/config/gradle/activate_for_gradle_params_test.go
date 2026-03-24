//nolint:maintidx
package gradleconfig

import (
	"errors"
	"fmt"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	commonmocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
)

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
			wantErr: fmt.Errorf(ErrFmtReadAuthConfig, common.ErrAuthTokenNotProvided).Error(),
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
			wantErr: fmt.Errorf(ErrFmtReadAuthConfig, common.ErrWorkspaceIDNotProvided).Error(),
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
					AuthToken: "WorkspaceIDValue:AuthTokenValue",
					Version:   consts.GradleCommonPluginDepVersion,
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
					AuthToken: "WorkspaceIDValue:AuthTokenValue",
					Version:   consts.GradleCommonPluginDepVersion,
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
					AuthToken: "WorkspaceIDValue:AuthTokenValue",
					Version:   consts.GradleCommonPluginDepVersion,
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
					AuthToken: "WorkspaceIDValue:AuthTokenValue",
					Version:   consts.GradleCommonPluginDepVersion,
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
				"BITRISE_APP_SLUG":                 "AppSlugValue",
			},
			want: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:  "WorkspaceIDValue:AuthTokenValue",
					AppSlug:    "AppSlugValue",
					CIProvider: "bitrise",
					Version:    consts.GradleCommonPluginDepVersion,
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
				"BITRISE_APP_SLUG":                 "AppSlugValue",
			},
			want: TemplateInventory{
				Common: PluginCommonTemplateInventory{
					AuthToken:  "WorkspaceIDValue:AuthTokenValue",
					Debug:      true,
					AppSlug:    "AppSlugValue",
					CIProvider: "bitrise",
					Version:    consts.GradleCommonPluginDepVersion,
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
