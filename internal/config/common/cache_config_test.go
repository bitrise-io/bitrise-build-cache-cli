package common

import (
	"reflect"
	"testing"
)

func TestNewCacheConfig(t *testing.T) {
	type args struct {
		envProvider EnvProviderFunc
	}
	tests := []struct {
		name string
		args args
		want CacheConfig
	}{
		{
			name: "Unknown CI provider",
			args: args{
				envProvider: createEnvProvider(map[string]string{}),
			},
			want: CacheConfig{},
		},
		{
			name: "Bitrise",
			args: args{
				envProvider: createEnvProvider(map[string]string{
					"BITRISE_IO":         "true",
					"GIT_REPOSITORY_URL": "git/repo/url",
				}),
			},
			want: CacheConfig{
				CIProvider: CIProviderBitrise,
				RepoURL:    "git/repo/url",
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
			want: CacheConfig{
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
			want: CacheConfig{
				CIProvider: CIProviderGitHubActions,
				RepoURL:    "https://github.com/owner/repo",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewCacheConfig(tt.args.envProvider); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewCacheConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}
