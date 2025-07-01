//nolint:maintidx
package gradleconfig

import (
	"errors"
	"fmt"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
			wantErr: fmt.Errorf(ErrFmtReadAutConfig, common.ErrAuthTokenNotProvided).Error(),
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
			wantErr: fmt.Errorf(ErrFmtReadAutConfig, common.ErrWorkspaceIDNotProvided).Error(),
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
			envProvider := func(key string) string { return tt.envVars[key] }
			got, err := tt.params.TemplateInventory(mockLogger, envProvider, tt.debug)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
