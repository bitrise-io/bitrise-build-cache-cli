package bazelconfig

import (
	_ "embed"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateBazelrc(t *testing.T) {
	t.Parallel()

	type args struct {
		endpointURL         string
		rbeEndpointURL      string
		workspaceID         string
		authToken           string
		timestamps          bool
		cacheConfigMetadata common.CacheConfigMetadata
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr string
	}{
		{
			name:    "Empty params - missing AuthToken",
			args:    args{},
			wantErr: "generate bazelrc, error: AuthToken not provided",
			want:    ``,
		},
		{
			name:    "Missing EndpointURL",
			args:    args{authToken: "4uth70k3n"},
			wantErr: "generate bazelrc, error: EndpointURL not provided",
			want:    ``,
		},
		{
			name: "Minimum required params provided",
			args: args{
				authToken:   "4uth70k3n",
				endpointURL: "grpcs://TESTENDPOINT.bitrise.io",
			},
			wantErr: "",
			want: `build --remote_cache=grpcs://TESTENDPOINT.bitrise.io
build --remote_timeout=600s
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --remote_header=authorization="Bearer 4uth70k3n"
build --bes_header=authorization="Bearer 4uth70k3n"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --build_event_publish_all_actions
`,
		},
		{
			name: "With RBE endpoint",
			args: args{
				endpointURL:    "grpcs://TESTENDPOINT.bitrise.io",
				rbeEndpointURL: "grpcs://RBE.bitrise.io",
				authToken:      "4uth70k3n",
			},
			wantErr: "",
			want: `build --remote_cache=grpcs://TESTENDPOINT.bitrise.io
build --remote_executor=grpcs://RBE.bitrise.io
build --remote_timeout=600s
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --remote_header=authorization="Bearer 4uth70k3n"
build --bes_header=authorization="Bearer 4uth70k3n"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --build_event_publish_all_actions
`,
		},
		{
			name: "All params defined",
			args: args{
				endpointURL:    "grpcs://TESTENDPOINT.bitrise.io",
				rbeEndpointURL: "grpcs://RBE.bitrise.io",
				workspaceID:    "W0rkSp4ceID",
				authToken:      "4uth70k3n",
				cacheConfigMetadata: common.CacheConfigMetadata{
					CIProvider: "BestCI",
					RepoURL:    "https://github.com/some/repo",
					// BitriseCI specific
					BitriseAppID:        "BitriseAppID1",
					BitriseWorkflowName: "BitriseWorkflowName1",
					BitriseBuildID:      "BitriseBuildID1",
				},
			},
			wantErr: "",
			want: `build --remote_cache=grpcs://TESTENDPOINT.bitrise.io
build --remote_executor=grpcs://RBE.bitrise.io
build --remote_timeout=600s
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --remote_header='x-org-id=W0rkSp4ceID'
build --bes_header='x-org-id=W0rkSp4ceID'
build --remote_header=authorization="Bearer 4uth70k3n"
build --bes_header=authorization="Bearer 4uth70k3n"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=BestCI
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --build_event_publish_all_actions
build --remote_header='x-ci-provider=BestCI'
build --bes_header='x-ci-provider=BestCI'
build --remote_header='x-repository-url=https://github.com/some/repo'
build --bes_header='x-repository-url=https://github.com/some/repo'
build --remote_header='x-app-id=BitriseAppID1'
build --bes_header='x-app-id=BitriseAppID1'
build --remote_header='x-workflow-name=BitriseWorkflowName1'
build --bes_header='x-workflow-name=BitriseWorkflowName1'
build --remote_header='x-flare-build-id=BitriseBuildID1'
build --bes_header='x-build-id=BitriseBuildID1'
`,
		},
		{
			name: "With Host metadata",
			args: args{
				endpointURL:    "grpcs://TESTENDPOINT.bitrise.io",
				rbeEndpointURL: "grpcs://RBE.bitrise.io",
				authToken:      "4uth70k3n",
				cacheConfigMetadata: common.CacheConfigMetadata{
					HostMetadata: common.HostMetadata{
						OS:             "Linux 22.04",
						MemSize:        1000,
						CPUCores:       4,
						Locale:         "en_US",
						DefaultCharset: "UTF-8",
					},
				},
			},
			wantErr: "",
			want: `build --remote_cache=grpcs://TESTENDPOINT.bitrise.io
build --remote_executor=grpcs://RBE.bitrise.io
build --remote_timeout=600s
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --remote_header=authorization="Bearer 4uth70k3n"
build --bes_header=authorization="Bearer 4uth70k3n"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --build_event_publish_all_actions
build --bes_header='x-os=Linux 22.04'
build --bes_header='x-locale=en_US'
build --bes_header='x-default-charset=UTF-8'
build --bes_header='x-cpu-cores=4'
build --bes_header='x-mem-size=1000'
`,
		},
		{
			name: "With timestamps",
			args: args{
				endpointURL: "grpcs://TESTENDPOINT.bitrise.io",
				authToken:   "4uth70k3n",
				timestamps:  true,
			},
			wantErr: "",
			want: `build --remote_cache=grpcs://TESTENDPOINT.bitrise.io
build --remote_timeout=600s
build --bes_timeout=2m
build --bes_upload_mode=wait_for_upload_complete
build --show_timestamps
build --remote_header=authorization="Bearer 4uth70k3n"
build --bes_header=authorization="Bearer 4uth70k3n"
build --remote_header=x-flare-buildtool=bazel
build --remote_header=x-flare-builduser=
build --bes_results_url=https://app.bitrise.io/build-cache/invocations/bazel/
build --bes_backend=grpcs://flare-bes.services.bitrise.io:443
build --build_event_publish_all_actions
`,
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := GenerateBazelrc(tt.args.endpointURL, tt.args.workspaceID, tt.args.authToken,
				tt.args.cacheConfigMetadata,
				Preferences{
					RBEEndpointURL:      tt.args.rbeEndpointURL,
					IsTimestampsEnabled: tt.args.timestamps,
				})

			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
