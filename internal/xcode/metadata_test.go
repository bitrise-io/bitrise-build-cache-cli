package xcode

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_SaveMetadata(t *testing.T) {
	type args struct {
		rootDir    string
		outputFile string
	}

	testRootDir, err := os.MkdirTemp("", "testRootDir")
	if err != nil {
		t.Logf("Error creating temp directory: %v", err)

		return
	}
	defer os.RemoveAll(testRootDir)

	testInputFile, err := os.CreateTemp(testRootDir, "test-file.swift")
	if err != nil {
		t.Logf("Error creating temp file: %v", err)

		return
	}
	testInputFile.Close()

	tests := []struct {
		name    string
		args    args
		wantErr string
		asserts func(t *testing.T)
	}{
		{
			name: "missing rootDir",
			args: args{
				rootDir:    "",
				outputFile: "metadata.json",
			},
			wantErr: "calculate file infos: missing rootDir",
		},
		{
			name: "missing fileName",
			args: args{
				rootDir:    testRootDir,
				outputFile: "",
			},
			wantErr: "missing output fileName",
		},
		{
			name: "ok",
			args: args{
				rootDir:    testRootDir,
				outputFile: "metadata.json",
			},
			asserts: func(t *testing.T) {
				t.Helper()

				md, err := LoadMetadata("metadata.json")
				require.NoError(t, err)

				require.Len(t, md.FileInfos, 1)
				fi := md.FileInfos[0]
				require.True(t, strings.HasPrefix(fi.Path, "test-file.swift"))
				require.NotEmpty(t, fi.Hash)
			},
		},
	}

	logger := setupTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SaveMetadata(tt.args.rootDir, tt.args.outputFile, logger)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			if tt.asserts != nil {
				tt.asserts(t)
			}
		})
	}
}
