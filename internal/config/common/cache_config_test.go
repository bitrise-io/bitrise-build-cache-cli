package common

import (
	"reflect"
	"testing"
)

func TestNewCacheConfigMetadata(t *testing.T) {
	type args struct {
		envProvider EnvProviderFunc
	}
	tests := []struct {
		name string
		args args
		want CacheConfigMetadata
	}{
		{
			name: "Unknown CI provider",
			args: args{
				envProvider: createEnvProvider(map[string]string{}),
			},
			want: CacheConfigMetadata{},
		},
		{
			name: "Bitrise CI",
			args: args{
				envProvider: createEnvProvider(map[string]string{
					"BITRISE_IO":                       "true",
					"GIT_REPOSITORY_URL":               "git/repo/url",
					"BITRISE_APP_SLUG":                 "BitriseAppID1",
					"BITRISE_BUILD_SLUG":               "BitriseBuildID1",
					"BITRISE_TRIGGERED_WORKFLOW_TITLE": "BitriseWorkflowName1",
					"BITRISE_STEP_EXECUTION_ID":        "BitriseStepID1",
				}),
			},
			want: CacheConfigMetadata{
				CIProvider:          CIProviderBitrise,
				RepoURL:             "git/repo/url",
				BitriseAppID:        "BitriseAppID1",
				BitriseBuildID:      "BitriseBuildID1",
				BitriseWorkflowName: "BitriseWorkflowName1",
				BitriseStepID:       "BitriseStepID1",
			},
		},
		{
			name: "CircleCI",
			args: args{
				envProvider: createEnvProvider(map[string]string{
					"CIRCLECI":              "true",
					"CIRCLE_REPOSITORY_URL": "git/repo/url",
				}),
			},
			want: CacheConfigMetadata{
				CIProvider: CIProviderCircleCI,
				RepoURL:    "git/repo/url",
			},
		},
		{
			name: "GitHub Actions",
			args: args{
				envProvider: createEnvProvider(map[string]string{
					"GITHUB_ACTIONS":    "true",
					"GITHUB_SERVER_URL": "https://github.com",
					"GITHUB_REPOSITORY": "owner/repo",
				}),
			},
			want: CacheConfigMetadata{
				CIProvider: CIProviderGitHubActions,
				RepoURL:    "https://github.com/owner/repo",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewCacheConfigMetadata(tt.args.envProvider); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewCacheConfigMetadata() = %v, want %v", got, tt.want)
			}
		})
	}
}
