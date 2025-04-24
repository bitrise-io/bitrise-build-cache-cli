package xcode

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func createEnvProvider(envs map[string]string) func(string) string {
	return func(s string) string { return envs[s] }
}

func Test_CreateMetadata(t *testing.T) {
	type args struct {
		rootDir    string
		outputFile string
	}

	testRootDir := t.TempDir()

	testInputFile, err := os.CreateTemp(testRootDir, "test-file.swift")
	if err != nil {
		t.Logf("Error creating temp file: %v", err)

		return
	}
	testInputFile.Close()

	tests := []struct {
		name        string
		args        args
		wantErr     string
		envProvider func(string) string
		asserts     func(t *testing.T, md *Metadata)
	}{
		{
			name: "missing rootDir",
			args: args{
				rootDir:    "",
				outputFile: "metadata.json",
			},
			wantErr: "missing project root directory path",
		},
		{
			name: "ok",
			args: args{
				rootDir:    testRootDir,
				outputFile: "metadata.json",
			},
			envProvider: createEnvProvider(map[string]string{
				"BITRISE_APP_SLUG":   "app-slug",
				"BITRISE_BUILD_SLUG": "build-slug",
				"BITRISE_GIT_COMMIT": "git-commit",
				"BITRISE_GIT_BRANCH": "git-branch",
			}),
			asserts: func(t *testing.T, md *Metadata) {
				t.Helper()

				require.Len(t, md.ProjectFiles.Files, 1)
				fi := md.ProjectFiles.Files[0]
				require.Contains(t, fi.Path, "test-file.swift")
				require.NotEmpty(t, fi.Hash)

				require.NotEmpty(t, md.CacheKey)
				require.NotEmpty(t, md.CreatedAt)
				require.Equal(t, "app-slug", md.AppID)
				require.Equal(t, "build-slug", md.BuildID)
				require.Equal(t, "git-commit", md.GitCommit)
				require.Equal(t, "git-branch", md.GitBranch)
			},
		},
	}

	logger := setupTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envProvider := tt.envProvider
			if envProvider == nil {
				envProvider = createEnvProvider(map[string]string{})
			}
			md, err := CreateMetadata(CreateMetadataParams{
				ProjectRootDirPath: tt.args.rootDir,
				DerivedDataPath:    tt.args.rootDir,
				CacheKey:           "some-key",
			}, envProvider, logger)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, md)
			}

			if tt.asserts != nil {
				tt.asserts(t, md)
			}
		})
	}
}
