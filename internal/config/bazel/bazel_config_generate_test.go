package bazelconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

func Test_Generate(t *testing.T) {
	tests := []struct {
		name      string
		inventory TemplateInventory
		want      string
		wantErr   string
	}{
		{
			name: "Basic configuration",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:   "AuthTokenValue",
					WorkspaceID: "WorkspaceIDValue",
					Debug:       false,
					AppSlug:     "AppSlugValue",
					CIProvider:  "CIProviderValue",
				},
				Cache: CacheTemplateInventory{
					Enabled:             true,
					EndpointURLWithPort: "grpcs://cache.services.bitrise.io:443",
					IsPushEnabled:       true,
				},
			},
			want:    expectedBasicConfig,
			wantErr: "",
		},
		{
			name: "Basic configuration with JWT",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:  "some-jwt-token",
					Debug:      false,
					AppSlug:    "AppSlugValue",
					CIProvider: "CIProviderValue",
				},
				Cache: CacheTemplateInventory{
					Enabled:             true,
					EndpointURLWithPort: "grpcs://cache.services.bitrise.io:443",
					IsPushEnabled:       true,
				},
			},
			want:    expectedBasicConfigJWT,
			wantErr: "",
		},
		{
			name: "Cache with push disabled",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:   "AuthTokenValue",
					WorkspaceID: "WorkspaceIDValue",
					Debug:       false,
					AppSlug:     "AppSlugValue",
					CIProvider:  "CIProviderValue",
				},
				Cache: CacheTemplateInventory{
					Enabled:             true,
					EndpointURLWithPort: "grpcs://cache.services.bitrise.io:443",
					IsPushEnabled:       false,
				},
			},
			want:    expectedConfigWithPushDisabled,
			wantErr: "",
		},
		{
			name: "With timestamps enabled",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:   "AuthTokenValue",
					WorkspaceID: "WorkspaceIDValue",
					Debug:       false,
					AppSlug:     "AppSlugValue",
					CIProvider:  "CIProviderValue",
					Timestamps:  true,
				},
				Cache: CacheTemplateInventory{
					Enabled:             true,
					EndpointURLWithPort: "grpcs://cache.services.bitrise.io:443",
					IsPushEnabled:       true,
				},
			},
			want:    expectedConfigWithTimestamps,
			wantErr: "",
		},
		{
			name: "BES disabled",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:    "AuthTokenValue",
					WorkspaceID:  "WorkspaceIDValue",
					Debug:        true,
					AppSlug:      "AppSlugValue",
					CIProvider:   "CIProviderValue",
					Timestamps:   true,
					BuildID:      "build-id-12345",
					RepoURL:      "https://repo-url",
					WorkflowName: "workflow-name",
				},
				Cache: CacheTemplateInventory{
					Enabled:             true,
					EndpointURLWithPort: "grpcs://cache.services.bitrise.io:443",
					IsPushEnabled:       true,
				},
				BES: BESTemplateInventory{
					Enabled: false,
				},
			},
			want:    expectedNoBESConfig,
			wantErr: "",
		},
		{
			name: "Full configuration with BES and RBE",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:    "AuthTokenValue",
					WorkspaceID:  "WorkspaceIDValue",
					Debug:        true,
					AppSlug:      "AppSlugValue",
					CIProvider:   "CIProviderValue",
					Timestamps:   true,
					BuildID:      "build-id-12345",
					RepoURL:      "https://repo-url",
					WorkflowName: "workflow-name",
					HostMetadata: HostMetadataInventory{
						OS:             "Linux prd-linux-use4c-87a9aa94-fcd4-4c5d-919c-f214f05a986c",
						Locale:         "en-US",
						DefaultCharset: "UTF-8",
						CPUCores:       8,
						MemSize:        1024,
					},
				},
				Cache: CacheTemplateInventory{
					Enabled:             true,
					EndpointURLWithPort: "grpcs://cache.services.bitrise.io:443",
					IsPushEnabled:       true,
				},
				BES: BESTemplateInventory{
					Enabled:             true,
					EndpointURLWithPort: "grpcs://flare-bes.services.bitrise.io:443",
				},
				RBE: RBETemplateInventory{
					Enabled:             true,
					EndpointURLWithPort: "grpcs://remote-execution.services.bitrise.io:6669",
				},
			},
			want:    expectedFullConfig,
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.inventory.GenerateBazelrc(utils.DefaultTemplateProxy())
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

const expectedBasicConfig = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer AuthTokenValue"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=CIProviderValue
build --remote_upload_local_results
build --remote_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=CIProviderValue'
`

const expectedBasicConfigJWT = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer some-jwt-token"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=CIProviderValue
build --remote_upload_local_results
build --remote_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=CIProviderValue'
`

const expectedConfigWithPushDisabled = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer AuthTokenValue"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=CIProviderValue
build --noremote_upload_local_results
build --remote_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=CIProviderValue'
`

const expectedConfigWithTimestamps = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer AuthTokenValue"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=CIProviderValue
build --remote_upload_local_results
build --show_timestamps
build --remote_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=CIProviderValue'
`

const expectedNoBESConfig = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer AuthTokenValue"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=CIProviderValue
build --remote_upload_local_results
build --verbose_failures
build --show_timestamps
build --remote_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=CIProviderValue'
build --remote_header='x-repository-url=https://repo-url'
build --remote_header='x-workflow-name=workflow-name'
build --remote_header='x-flare-build-id=build-id-12345'
`

const expectedFullConfig = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer AuthTokenValue"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=CIProviderValue
build --remote_upload_local_results
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --bes_header=authorization="Bearer AuthTokenValue"
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --build_event_publish_all_actions
build --remote_executor=grpcs://remote-execution.services.bitrise.io:6669
build --verbose_failures
build --show_timestamps
build --remote_header='x-org-id=WorkspaceIDValue'
build --bes_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --bes_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=CIProviderValue'
build --bes_header='x-ci-provider=CIProviderValue'
build --remote_header='x-repository-url=https://repo-url'
build --bes_header='x-repository-url=https://repo-url'
build --remote_header='x-workflow-name=workflow-name'
build --bes_header='x-workflow-name=workflow-name'
build --remote_header='x-flare-build-id=build-id-12345'
build --bes_header='x-build-id=build-id-12345'
build --bes_header='x-os=Linux prd-linux-use4c-87a9aa94-fcd4-4c5d-919c-f214f05a986c'
build --bes_header='x-locale=en-US'
build --bes_header='x-default-charset=UTF-8'
build --bes_header='x-cpu-cores=8'
build --bes_header='x-mem-size=1024'
`
