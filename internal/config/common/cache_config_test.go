// nolint: gocognit, gocyclo, funlen, maintidx
package common

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCacheConfigMetadata(t *testing.T) {
	t.Parallel()
	logger := log.NewLogger()

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
				"BITRISE_IO":                    "true",
				"BITRISE_APP_SLUG":              "BitriseAppID1",
				"BITRISE_BUILD_SLUG":            "BitriseBuildID1",
				"BITRISE_TRIGGERED_WORKFLOW_ID": "BitriseWorkflowName1",
				"GIT_REPOSITORY_URL":            "https://github.com/repo/url",
				"GIT_CLONE_COMMIT_HASH":         "abcdef1234567890",
				"BITRISE_GIT_BRANCH":            "main",
				"GIT_CLONE_COMMIT_AUTHOR_EMAIL": "john.doe@bitrise.io",
			},
			want: CacheConfigMetadata{
				CIProvider:          CIProviderBitrise,
				CLIVersion:          GetCLIVersion(logger),
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
				"CIRCLECI":                "true",
				"CIRCLE_PROJECT_REPONAME": "my-repo",
				"CIRCLE_WORKFLOW_ID":      "wf-123",
				"CIRCLE_JOB":              "build",
			},
			commandFunc: func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider:           CIProviderCircleCI,
				CLIVersion:           GetCLIVersion(logger),
				ExternalAppID:        "my-repo",
				ExternalBuildID:      "wf-123",
				ExternalWorkflowName: "build",
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
				CLIVersion: GetCLIVersion(logger),
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
				"GITHUB_REPOSITORY": "org/my-repo",
				"GITHUB_RUN_ID":     "run-456",
				"GITHUB_JOB":        "test",
			},
			commandFunc: func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider:           CIProviderGitHubActions,
				CLIVersion:           GetCLIVersion(logger),
				ExternalAppID:        "org/my-repo",
				ExternalBuildID:      "run-456",
				ExternalWorkflowName: "test",
			},
		},
		{
			name: "GitLab CI",
			envs: map[string]string{
				"GITLAB_CI":       "true",
				"CI_PROJECT_PATH": "group/my-project",
				"CI_PIPELINE_ID":  "pipeline-789",
				"CI_JOB_NAME":     "compile",
			},
			commandFunc: func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider:           CIProviderGitLabCI,
				CLIVersion:           GetCLIVersion(logger),
				ExternalAppID:        "group/my-project",
				ExternalBuildID:      "pipeline-789",
				ExternalWorkflowName: "compile",
			},
		},
		{
			name: "OS",
			envs: map[string]string{
				"BITRISE_IO":         "true",
				"BITRISE_BUILD_SLUG": "build1",
			},
			commandFunc: func(c string, _ ...string) (string, error) {
				if strings.Contains(c, "uname") {
					return "Linux", nil
				}

				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider:     CIProviderBitrise,
				CLIVersion:     GetCLIVersion(logger),
				BitriseBuildID: "build1",
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
				CLIVersion: GetCLIVersion(logger),
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
				"BITRISE_IO":         "true",
				"BITRISE_BUILD_SLUG": "build1",
				"LANG":               "en_US.UTF-8",
			},
			commandFunc: func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider:     CIProviderBitrise,
				CLIVersion:     GetCLIVersion(logger),
				BitriseBuildID: "build1",
				HostMetadata: HostMetadata{
					Locale:         "en_US",
					DefaultCharset: "UTF-8",
				},
			},
		},
		{
			name: "CPU",
			envs: map[string]string{
				"BITRISE_IO":         "true",
				"BITRISE_BUILD_SLUG": "build1",
			},
			commandFunc: func(c string, a ...string) (string, error) {
				if strings.Contains(c, "nproc") ||
					(strings.Contains(c, "sysctl") && slices.Contains(a, "hw.ncpu")) {
					return "4", nil
				}

				return "", nil
			},
			want: CacheConfigMetadata{
				CIProvider:     CIProviderBitrise,
				CLIVersion:     GetCLIVersion(logger),
				BitriseBuildID: "build1",
				HostMetadata: HostMetadata{
					CPUCores: 4,
				},
			},
		},
		{
			name: "Memory",
			envs: map[string]string{
				"BITRISE_IO":         "true",
				"BITRISE_BUILD_SLUG": "build1",
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
				CIProvider:     CIProviderBitrise,
				CLIVersion:     GetCLIVersion(logger),
				BitriseBuildID: "build1",
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
				CLIVersion: GetCLIVersion(logger),
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

			got := NewMetadata(tt.envs, "",
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

func TestRedactBitriseEnvs_alwaysRedactedKeys(t *testing.T) {
	t.Parallel()

	envs := map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":          "raw-token-1",
		"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "raw-token-2",
		"BITRISE_API_TOKEN":                       "raw-token-3",
		"BUILD_TRIGGER_TOKEN":                     "raw-token-4",
		"PATH":                                    "/usr/bin",
	}

	redactBitriseEnvs(envs)

	for _, key := range []string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN",
		"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN",
		"BITRISE_API_TOKEN",
		"BUILD_TRIGGER_TOKEN",
	} {
		assert.NotEqual(t, "raw-token", envs[key][:9], "%s must be redacted", key)
		assert.Contains(t, envs[key], "<sha256@", "%s must be redacted with sha256 marker", key)
	}
	assert.Equal(t, "/usr/bin", envs["PATH"], "non-sensitive envs must be passed through")
}

func TestRedactBitriseEnvs_tokenValuePrefixSafetyNet(t *testing.T) {
	t.Parallel()

	envs := map[string]string{
		"SOME_UNKNOWN_TOKEN_KEY": "bitpat_ABCDEF",
		"ANOTHER_UNKNOWN_KEY":    "bitwat_XYZ123",
		"UNRELATED":              "plain-value",
	}

	redactBitriseEnvs(envs)

	assert.Contains(t, envs["SOME_UNKNOWN_TOKEN_KEY"], "<sha256@",
		"bitpat_ values must be redacted even when the key is unknown")
	assert.Contains(t, envs["ANOTHER_UNKNOWN_KEY"], "<sha256@",
		"bitwat_ values must be redacted even when the key is unknown")
	assert.Equal(t, "plain-value", envs["UNRELATED"], "non-token values must be passed through")
}

func TestRedactBitriseEnvs_secretKeyListStillHonoured(t *testing.T) {
	t.Parallel()

	envs := map[string]string{
		"BITRISE_SECRET_ENV_KEY_LIST": "MY_SECRET,OTHER",
		"MY_SECRET":                   "s1",
		"OTHER":                       "s2",
		"NOT_SECRET":                  "kept",
	}

	redactBitriseEnvs(envs)

	assert.Contains(t, envs["MY_SECRET"], "<sha256@")
	assert.Contains(t, envs["OTHER"], "<sha256@")
	assert.Equal(t, "kept", envs["NOT_SECRET"])
}

func TestResolveDefaultBranch(t *testing.T) {
	t.Parallel()
	logger := log.NewLogger()

	gitHubEventPath := filepath.Join(t.TempDir(), "event.json")
	require.NoError(t, os.WriteFile(gitHubEventPath, []byte(`{"repository":{"default_branch":"trunk"}}`), 0o600))

	remoteHead := func(_ string, _ ...string) (string, error) { return "origin/main\n", nil }
	noRemoteHead := func(_ string, _ ...string) (string, error) { return "", errors.New("no upstream") }

	tests := []struct {
		name        string
		commandFunc CommandFunc
		envs        map[string]string
		want        string
	}{
		{
			name:        "Bitrise leaves it to the API's project record",
			commandFunc: remoteHead,
			envs:        map[string]string{"BITRISE_IO": "true", "BITRISE_BUILD_SLUG": "build-1"},
			want:        "",
		},
		{
			name:        "GitLab publishes it directly",
			commandFunc: noRemoteHead,
			envs:        map[string]string{"GITLAB_CI": "true", "CI_DEFAULT_BRANCH": "develop"},
			want:        "develop",
		},
		{
			name:        "GitHub Actions reads the event payload",
			commandFunc: noRemoteHead,
			envs:        map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_PATH": gitHubEventPath},
			want:        "trunk",
		},
		{
			name:        "GitHub Actions falls back to remote HEAD without a payload",
			commandFunc: remoteHead,
			envs:        map[string]string{"GITHUB_ACTIONS": "true"},
			want:        "main",
		},
		{
			name:        "CircleCI publishes nothing, so remote HEAD answers",
			commandFunc: remoteHead,
			envs:        map[string]string{"CIRCLECI": "true"},
			want:        "main",
		},
		{
			name:        "unknown when nothing can resolve it",
			commandFunc: noRemoteHead,
			envs:        map[string]string{"CIRCLECI": "true"},
			want:        "",
		},
		{
			name:        "unreadable GitHub payload falls back rather than failing",
			commandFunc: noRemoteHead,
			envs:        map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_EVENT_PATH": "/nonexistent/event.json"},
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, resolveDefaultBranch(logger, tt.commandFunc, tt.envs))
		})
	}
}

func TestIsCI(t *testing.T) {
	buildHub := map[string]string{
		"BITRISE_IO":                       "true",
		"BITRISEIO_BUILD_HUB_VM_TOKEN":     "vm-token",
		"BITRISEIO_BUILD_HUB_VM_TOKEN_URL": "https://example.com",
	}

	assert.False(t, IsCI(map[string]string{}))
	assert.True(t, IsCI(map[string]string{"GITHUB_ACTIONS": "true"}))
	assert.True(t, IsCI(buildHub), "a Build Hub runner under an unrecognised CI is still CI")
	assert.Empty(t, DetectCIProvider(buildHub), "the provider name stays empty: nothing identifies which CI it is")
	assert.False(t, IsCI(map[string]string{"BITRISEIO_BUILD_HUB_VM_TOKEN": "vm-token"}), "half the pair is not Build Hub")
}
