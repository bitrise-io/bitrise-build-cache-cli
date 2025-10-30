// nolint: gocognit, gocyclo, funlen, maintidx
package common

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
)

func TestNewCacheConfigMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		commandFunc CommandFunc
		envs        map[string]string
		want        CacheConfigMetadata
	}{
		{
			name: "Bitrise CI",
			commandFunc: func(_ string, _ ...string) (string, error) {
				return "", errors.New("some error") // So that we get the git params from env vars
			},
			envs: map[string]string{
				"BITRISE_IO":                       "true",
				"BITRISE_APP_SLUG":                 "BitriseAppID1",
				"BITRISE_BUILD_SLUG":               "BitriseBuildID1",
				"BITRISE_TRIGGERED_WORKFLOW_TITLE": "BitriseWorkflowName1",
				"GIT_REPOSITORY_URL":               "https://github.com/repo/url",
				"GIT_CLONE_COMMIT_HASH":            "abcdef1234567890",
				"BITRISE_GIT_BRANCH":               "main",
				"GIT_CLONE_COMMIT_AUTHOR_EMAIL":    "john.doe@bitrise.io",
			},
			want: CacheConfigMetadata{
				CIProvider:          CIProviderBitrise,
				BitriseAppID:        "BitriseAppID1",
				BitriseBuildID:      "BitriseBuildID1",
				BitriseWorkflowName: "BitriseWorkflowName1",
				GitMetadata: GitMetadata{
					RepoURL:     "https://github.com/repo/url",
					CommitHash:  "abcdef1234567890",
					Branch:      "main",
					CommitEmail: "john.doe@bitrise.io",
				},
			},
		},
		{
			name: "CircleCI",
			envs: map[string]string{
				"CIRCLECI": "true",
			},
			commandFunc: func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider: CIProviderCircleCI,
			},
		},
		{
			name: "Envs",
			envs: map[string]string{
				"BITRISE_SECRET_ENV_KEY_LIST": "MY_SECRET,MY_SECRET2,MY_SECRET3",
				"CIRCLECI":                    "true",
				"MY_SECRET":                   "val1",
				"MY_SECRET2":                  "val2",
				"MY_SECRET3":                  "val2",
			},
			commandFunc: func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider: CIProviderCircleCI,
				RedactedEnvs: map[string]string{
					"MY_SECRET":                   "<sha256@49bf1460>",
					"MY_SECRET2":                  "<sha256@67171c3a>",
					"MY_SECRET3":                  "<sha256@cdd23b1f>", // key is part of the hash
					"BITRISE_SECRET_ENV_KEY_LIST": "MY_SECRET,MY_SECRET2,MY_SECRET3",
					"CIRCLECI":                    "true",
				},
			},
		},
		{
			name: "GitHub Actions",
			envs: map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_SERVER_URL": "https://github.com",
			},
			commandFunc: func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider: CIProviderGitHubActions,
			},
		},
		{
			name: "OS",
			envs: map[string]string{
				"BITRISE_IO": "true",
			},
			commandFunc: func(c string, _ ...string) (string, error) {
				if strings.Contains(c, "uname") {
					return "Linux", nil
				}

				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider: CIProviderBitrise,
				HostMetadata: HostMetadata{
					OS: "Linux",
				},
			},
		},
		{
			name: "Non-CI OS",
			envs: map[string]string{
				"LANG": "en_US.UTF-8",
			},
			commandFunc: func(c string, a ...string) (string, error) {
				hasMemTotal := slices.ContainsFunc(a, func(s string) bool {
					return strings.Contains(s, "MemTotal")
				})
				hasMemSize := strings.Contains(c, "sysctl") && slices.Contains(a, "hw.memsize")

				if hasMemTotal {
					return "1", nil
				}
				if hasMemSize {
					return "1000", nil
				}

				if strings.Contains(c, "nproc") ||
					(strings.Contains(c, "sysctl") && slices.Contains(a, "hw.ncpu")) {
					return "4", nil
				}

				if strings.Contains(c, "uname") {
					return "Linux", nil
				}

				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider: "",
				HostMetadata: HostMetadata{
					OS:             "Linux",
					Locale:         "en_US",
					DefaultCharset: "UTF-8",
					CPUCores:       4,
					MemSize:        1000,
				},
			},
		},
		{
			name: "Locale",
			envs: map[string]string{
				"BITRISE_IO": "true",
				"LANG":       "en_US.UTF-8",
			},
			commandFunc: func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider: CIProviderBitrise,
				HostMetadata: HostMetadata{
					Locale:         "en_US",
					DefaultCharset: "UTF-8",
				},
			},
		},
		{
			name: "CPU",
			envs: map[string]string{
				"BITRISE_IO": "true",
			},
			commandFunc: func(c string, a ...string) (string, error) {
				if strings.Contains(c, "nproc") ||
					(strings.Contains(c, "sysctl") && slices.Contains(a, "hw.ncpu")) {
					return "4", nil
				}

				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider: CIProviderBitrise,
				HostMetadata: HostMetadata{
					CPUCores: 4,
				},
			},
		},
		{
			name: "Memory",
			envs: map[string]string{
				"BITRISE_IO": "true",
			},
			commandFunc: func(c string, a ...string) (string, error) {
				hasMemTotal := slices.ContainsFunc(a, func(s string) bool {
					return strings.Contains(s, "MemTotal")
				})
				hasMemSize := strings.Contains(c, "sysctl") && slices.Contains(a, "hw.memsize")

				if hasMemTotal {
					return "1", nil
				}
				if hasMemSize {
					return "1000", nil
				}

				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider: CIProviderBitrise,
				HostMetadata: HostMetadata{
					MemSize: 1000,
				},
			},
		},
		{
			name: "Git",
			envs: map[string]string{},
			commandFunc: func(c string, a ...string) (string, error) {
				if strings.Contains(c, "git") && slices.Contains(a, "remote.origin.url") {
					return "https://github.com/repo/url", nil
				}
				if strings.Contains(c, "git") && slices.Contains(a, "HEAD") {
					return "abcdef12356", nil
				}
				if strings.Contains(c, "git") && slices.Contains(a, "branch") {
					return "main", nil
				}
				if strings.Contains(c, "git") && slices.Contains(a, "show") {
					return "john.doe@bitrise.io", nil
				}

				return "", nil
			},
			want: CacheConfigMetadata{
				GitMetadata: GitMetadata{
					RepoURL:     "https://github.com/repo/url",
					CommitHash:  "abcdef12356",
					Branch:      "main",
					CommitEmail: "john.doe@bitrise.io",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := NewMetadata(tt.envs,
				tt.commandFunc,
				log.NewLogger())

			if tt.want.RedactedEnvs != nil {
				assert.Equal(t, tt.want.RedactedEnvs, got.RedactedEnvs)
			}
			got.RedactedEnvs = nil
			tt.want.RedactedEnvs = nil

			// Reset fields that we're not interested in comparing
			if tt.want.HostMetadata.MemSize == 0 {
				got.HostMetadata.MemSize = 0
			}
			if tt.want.HostMetadata.CPUCores == 0 {
				got.HostMetadata.CPUCores = 0
			}
			if tt.want.HostMetadata.Username == "" {
				got.HostMetadata.Username = ""
			}
			if tt.want.HostMetadata.Hostname == "" {
				got.HostMetadata.Hostname = ""
			}
			if tt.want.HostMetadata.OS == "" {
				got.HostMetadata.OS = ""
			}
			if tt.want.HostMetadata.Locale == "" {
				got.HostMetadata.Locale = ""
			}
			if tt.want.GitMetadata.CommitHash == "" {
				got.GitMetadata.CommitHash = ""
			}
			if tt.want.GitMetadata.Branch == "" {
				got.GitMetadata.Branch = ""
			}
			if tt.want.GitMetadata.CommitEmail == "" {
				got.GitMetadata.CommitEmail = ""
			}
			if tt.want.GitMetadata.RepoURL == "" {
				got.GitMetadata.RepoURL = ""
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewMetadata() = %v, want %v", got, tt.want)
			}
		})
	}
}
