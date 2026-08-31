package bazelconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func assertBazelrc(t *testing.T, inv TemplateInventory, want, wantErr string) {
	t.Helper()
	got, err := inv.GenerateBazelrc(utils.DefaultTemplateProxy())
	if wantErr != "" {
		require.EqualError(t, err, wantErr)
	} else {
		require.NoError(t, err)
	}
	assert.Equal(t, want, got)
}

func Test_Generate_BasicPermutations(t *testing.T) {
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
			assertBazelrc(t, tt.inventory, tt.want, tt.wantErr)
		})
	}
}

func Test_Generate_LocalDevHelper(t *testing.T) {
	tests := []struct {
		name      string
		inventory TemplateInventory
		want      string
		wantErr   string
	}{
		{
			name: "Local dev with credential helper (CLIPath set, no CIProvider)",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:   "AuthTokenValue",
					WorkspaceID: "WorkspaceIDValue",
					AppSlug:     "AppSlugValue",
					CIProvider:  "",
					CLIPath:     "/usr/local/bin/bitrise-build-cache",
					HostMetadata: HostMetadataInventory{
						Username: "jane.doe",
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
			},
			want:    expectedLocalHelperConfig,
			wantErr: "",
		},
		{
			// ~/.bazelrc is machine-global, so a stored repo URL would attribute every
			// Bazel project on the machine to this one. The helper sends it instead.
			name: "Credential helper drops the resolved repo URL from the bazelrc",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:   "AuthTokenValue",
					WorkspaceID: "WorkspaceIDValue",
					AppSlug:     "AppSlugValue",
					RepoURL:     "https://repo-url",
					CLIPath:     "/usr/local/bin/bitrise-build-cache",
					HostMetadata: HostMetadataInventory{
						Username: "jane.doe",
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
			},
			want:    expectedLocalHelperConfig,
			wantErr: "",
		},
		{
			name: "Local dev with no CIProvider and no resolved Username emits empty builduser",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:   "AuthTokenValue",
					WorkspaceID: "WorkspaceIDValue",
					AppSlug:     "AppSlugValue",
					CIProvider:  "",
					CLIPath:     "/usr/local/bin/bitrise-build-cache",
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
			},
			want:    expectedLocalHelperConfigNoUsername,
			wantErr: "",
		},
		{
			// The bare name is what `activate` passes when the running binary is on a
			// temporary path. Dropping the helper here would silently fall back to
			// writing the token into ~/.bazelrc.
			name: "Local dev with the bare binary name resolves the helper via $PATH",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:   "AuthTokenValue",
					WorkspaceID: "WorkspaceIDValue",
					AppSlug:     "AppSlugValue",
					CIProvider:  "",
					CLIPath:     "bitrise-build-cache",
					HostMetadata: HostMetadataInventory{
						Username: "jane.doe",
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
			},
			want:    strings.ReplaceAll(expectedLocalHelperConfig, "/usr/local/bin/bitrise-build-cache", "bitrise-build-cache"),
			wantErr: "",
		},
		{
			name: "Local dev credential helper: cache disabled, BES enabled",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:  "AuthTokenValue",
					AppSlug:    "AppSlugValue",
					CIProvider: "",
					CLIPath:    "/usr/local/bin/bitrise-build-cache",
				},
				Cache: CacheTemplateInventory{Enabled: false},
				BES: BESTemplateInventory{
					Enabled:             true,
					EndpointURLWithPort: "grpcs://flare-bes.services.bitrise.io:443",
				},
			},
			want:    expectedHelperCacheDisabled,
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertBazelrc(t, tt.inventory, tt.want, tt.wantErr)
		})
	}
}

func Test_Generate_CI(t *testing.T) {
	tests := []struct {
		name      string
		inventory TemplateInventory
		want      string
		wantErr   string
	}{
		{
			name: "CLIPath set on CI uses the credential helper too",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:   "AuthTokenValue",
					WorkspaceID: "WorkspaceIDValue",
					AppSlug:     "AppSlugValue",
					CIProvider:  "bitrise",
					CLIPath:     "/usr/local/bin/bitrise-build-cache",
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
			},
			want:    expectedCIHelperConfig,
			wantErr: "",
		},
		{
			name: "CI without a reachable CLI falls back to the literal token",
			inventory: TemplateInventory{
				Common: CommonTemplateInventory{
					AuthToken:   "AuthTokenValue",
					WorkspaceID: "WorkspaceIDValue",
					AppSlug:     "AppSlugValue",
					CIProvider:  "bitrise",
					CLIPath:     "",
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
			},
			want:    expectedCIFallbackHeaders,
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertBazelrc(t, tt.inventory, tt.want, tt.wantErr)
		})
	}
}

const expectedBasicConfig = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer AuthTokenValue"
build --remote_header=x-flare-buildtool=bazel
build --remote_header='x-flare-builduser=CIProviderValue'
build --remote_upload_local_results
build --remote_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=CIProviderValue'
`

const expectedBasicConfigJWT = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer some-jwt-token"
build --remote_header=x-flare-buildtool=bazel
build --remote_header='x-flare-builduser=CIProviderValue'
build --remote_upload_local_results
build --remote_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=CIProviderValue'
`

const expectedConfigWithPushDisabled = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer AuthTokenValue"
build --remote_header=x-flare-buildtool=bazel
build --remote_header='x-flare-builduser=CIProviderValue'
build --noremote_upload_local_results
build --remote_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=CIProviderValue'
`

const expectedConfigWithTimestamps = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer AuthTokenValue"
build --remote_header=x-flare-buildtool=bazel
build --remote_header='x-flare-builduser=CIProviderValue'
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
build --remote_header='x-flare-builduser=CIProviderValue'
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
build --remote_header='x-flare-builduser=CIProviderValue'
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

const expectedLocalHelperConfig = `build --credential_helper=*.services.bitrise.io=/usr/local/bin/bitrise-build-cache
build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=x-flare-buildtool=bazel
build --remote_header='x-flare-builduser=jane.doe'
build --remote_upload_local_results
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --build_event_publish_all_actions
build --remote_header='x-org-id=WorkspaceIDValue'
build --bes_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --bes_header='x-app-id=AppSlugValue'
`

const expectedLocalHelperConfigNoUsername = `build --credential_helper=*.services.bitrise.io=/usr/local/bin/bitrise-build-cache
build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=x-flare-buildtool=bazel
build --remote_header='x-flare-builduser='
build --remote_upload_local_results
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --build_event_publish_all_actions
build --remote_header='x-org-id=WorkspaceIDValue'
build --bes_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --bes_header='x-app-id=AppSlugValue'
`

const expectedCIHelperConfig = `build --credential_helper=*.services.bitrise.io=/usr/local/bin/bitrise-build-cache
build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=x-flare-buildtool=bazel
build --remote_header='x-flare-builduser=bitrise'
build --remote_upload_local_results
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --build_event_publish_all_actions
build --remote_header='x-org-id=WorkspaceIDValue'
build --bes_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --bes_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=bitrise'
build --bes_header='x-ci-provider=bitrise'
`

const expectedCIFallbackHeaders = `build --remote_cache=grpcs://cache.services.bitrise.io:443
build --remote_timeout=600s
build --remote_header=authorization="Bearer AuthTokenValue"
build --remote_header=x-flare-buildtool=bazel
build --remote_header='x-flare-builduser=bitrise'
build --remote_upload_local_results
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --bes_header=authorization="Bearer AuthTokenValue"
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --build_event_publish_all_actions
build --remote_header='x-org-id=WorkspaceIDValue'
build --bes_header='x-org-id=WorkspaceIDValue'
build --remote_header='x-app-id=AppSlugValue'
build --bes_header='x-app-id=AppSlugValue'
build --remote_header='x-ci-provider=bitrise'
build --bes_header='x-ci-provider=bitrise'
`

const expectedHelperCacheDisabled = `build --credential_helper=*.services.bitrise.io=/usr/local/bin/bitrise-build-cache

build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --build_event_publish_all_actions
build --bes_header='x-app-id=AppSlugValue'
`
