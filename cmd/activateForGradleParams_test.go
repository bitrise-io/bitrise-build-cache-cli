//nolint:maintidx
package cmd

import (
	"fmt"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_activateForGradleParams(t *testing.T) {
	prep := func(debug bool) log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()

		isDebugLogMode = debug

		return mockLogger
	}

	tests := []struct {
		name    string
		debug   bool
		params  ActivateForGradleParams
		envVars map[string]string
		want    gradleconfig.TemplateInventory
		wantErr string
	}{
		{
			name: "no auth token",
			params: ActivateForGradleParams{
				Cache:      CacheParams{Enabled: false},
				Analytics:  AnalyticsParams{Enabled: false},
				TestDistro: TestDistroParams{Enabled: false},
			},
			envVars: map[string]string{},
			wantErr: fmt.Errorf(errFmtReadAutConfig, common.ErrAuthTokenNotProvided).Error(),
		},
		{
			name: "no workspaceID",
			params: ActivateForGradleParams{
				Cache:      CacheParams{Enabled: false},
				Analytics:  AnalyticsParams{Enabled: false},
				TestDistro: TestDistroParams{Enabled: false},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN": "AuthTokenValue",
			},
			wantErr: fmt.Errorf(errFmtReadAutConfig, common.ErrWorkspaceIDNotProvided).Error(),
		},
		{
			name: "no plugins",
			params: ActivateForGradleParams{
				Cache:      CacheParams{Enabled: false},
				Analytics:  AnalyticsParams{Enabled: false},
				TestDistro: TestDistroParams{Enabled: false},
			},
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
			},
			want: gradleconfig.TemplateInventory{
				Common: gradleconfig.PluginCommonTemplateInventory{
					AuthToken: "WorkspaceIDValue:AuthTokenValue",
				},
				Cache: gradleconfig.CacheTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
				Analytics: gradleconfig.AnalyticsTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
				TestDistro: gradleconfig.TestDistroTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
			},
		},
		{
			name: "dependency only plugins",
			params: ActivateForGradleParams{
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
			want: gradleconfig.TemplateInventory{
				Common: gradleconfig.PluginCommonTemplateInventory{
					AuthToken: "WorkspaceIDValue:AuthTokenValue",
				},
				Cache: gradleconfig.CacheTemplateInventory{
					Usage:   gradleconfig.UsageLevelDependency,
					Version: consts.GradleRemoteBuildCachePluginDepVersion,
				},
				Analytics: gradleconfig.AnalyticsTemplateInventory{
					Usage:   gradleconfig.UsageLevelDependency,
					Version: consts.GradleAnalyticsPluginDepVersion,
				},
				TestDistro: gradleconfig.TestDistroTemplateInventory{
					Usage:   gradleconfig.UsageLevelDependency,
					Version: consts.GradleTestDistributionPluginDepVersion,
				},
			},
		},
		{
			name: "activate cache",
			params: ActivateForGradleParams{
				Cache: CacheParams{
					Enabled:         true,
					JustDependency:  true, // gets overridden by enable
					ValidationLevel: string(gradleconfig.CacheValidationLevelError),
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
			want: gradleconfig.TemplateInventory{
				Common: gradleconfig.PluginCommonTemplateInventory{
					AuthToken: "WorkspaceIDValue:AuthTokenValue",
				},
				Cache: gradleconfig.CacheTemplateInventory{
					Usage:               gradleconfig.UsageLevelDependency,
					Version:             consts.GradleRemoteBuildCachePluginDepVersion,
					EndpointURLWithPort: "EndpointValue",
					IsPushEnabled:       true,
					ValidationLevel:     string(gradleconfig.CacheValidationLevelError),
				},
				Analytics: gradleconfig.AnalyticsTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
				TestDistro: gradleconfig.TestDistroTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
			},
		},
		{
			name: "given invalid cache validation level cache activation throws error",
			params: ActivateForGradleParams{
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
			wantErr: fmt.Errorf(errFmtCacheConfigCreation, errInvalidCacheLevel).Error(),
		},
		{
			name: "activate analytics",
			params: ActivateForGradleParams{
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
			want: gradleconfig.TemplateInventory{
				Common: gradleconfig.PluginCommonTemplateInventory{
					AuthToken: "WorkspaceIDValue:AuthTokenValue",
				},
				Cache: gradleconfig.CacheTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
				Analytics: gradleconfig.AnalyticsTemplateInventory{
					Usage:        gradleconfig.UsageLevelEnabled,
					Version:      consts.GradleAnalyticsPluginDepVersion,
					Endpoint:     consts.GradleAnalyticsEndpoint,
					Port:         consts.GradleAnalyticsPort,
					HTTPEndpoint: consts.GradleAnalyticsHTTPEndpoint,
				},
				TestDistro: gradleconfig.TestDistroTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
			},
		},
		{
			name: "activate test distro",
			params: ActivateForGradleParams{
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
			want: gradleconfig.TemplateInventory{
				Common: gradleconfig.PluginCommonTemplateInventory{
					AuthToken:  "WorkspaceIDValue:AuthTokenValue",
					AppSlug:    "AppSlugValue",
					CIProvider: "bitrise",
				},
				Cache: gradleconfig.CacheTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
				Analytics: gradleconfig.AnalyticsTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
				TestDistro: gradleconfig.TestDistroTemplateInventory{
					Usage:      gradleconfig.UsageLevelEnabled,
					Version:    consts.GradleTestDistributionPluginDepVersion,
					Endpoint:   consts.GradleTestDistributionEndpoint,
					KvEndpoint: consts.GradleTestDistributionKvEndpoint,
					Port:       consts.GradleTestDistributionPort,
					LogLevel:   "warning",
				},
			},
		},
		{
			name: "activating test distro while missing app slug throws error",
			params: ActivateForGradleParams{
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
			},
			wantErr: fmt.Errorf(errFmtTestDistroConfigCreation, errTestDistroAppSlug).Error(),
		},
		{
			name:  "activate plugins with debug mode",
			debug: true,
			params: ActivateForGradleParams{
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
			want: gradleconfig.TemplateInventory{
				Common: gradleconfig.PluginCommonTemplateInventory{
					AuthToken:  "WorkspaceIDValue:AuthTokenValue",
					Debug:      true,
					AppSlug:    "AppSlugValue",
					CIProvider: "bitrise",
				},
				Cache: gradleconfig.CacheTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
				Analytics: gradleconfig.AnalyticsTemplateInventory{
					Usage: gradleconfig.UsageLevelNone,
				},
				TestDistro: gradleconfig.TestDistroTemplateInventory{
					Usage:      gradleconfig.UsageLevelEnabled,
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
			mockLogger := prep(tt.debug)
			envProvider := func(key string) string { return tt.envVars[key] }
			got, err := tt.params.templateInventory(mockLogger, envProvider)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
