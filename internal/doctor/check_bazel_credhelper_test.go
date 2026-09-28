//go:build unit

package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBazelCredHelperCheck(t *testing.T) {
	tests := []struct {
		name       string
		rcContents string
		cliOnPATH  bool
		wantState  State
		wantDetail string
	}{
		{
			name:       "no pin, silent OK",
			rcContents: "",
			cliOnPATH:  true,
			wantState:  StateOK,
			wantDetail: "no repo-level",
		},
		{
			name:       "pin + CLI present → warn",
			rcContents: "build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n",
			cliOnPATH:  true,
			wantState:  StateWarn,
			wantDetail: "credential-helper pin",
		},
		{
			name:       "pin + CLI missing → error",
			rcContents: "build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n",
			cliOnPATH:  false,
			wantState:  StateError,
			wantDetail: "is not on PATH",
		},
		{
			name:       "unrelated helper for non-Bitrise scope is OK",
			rcContents: "build --credential_helper=other.example.com=/some/bin\n",
			cliOnPATH:  false,
			wantState:  StateOK,
			wantDetail: "no repo-level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			// Init as a git repo so ScanForPinnedHelper's default git-tracked
			// filter sees the .bazelrc as tracked.
			runGit(t, dir, "init", "-q")

			if tt.rcContents != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".bazelrc"),
					[]byte(tt.rcContents), 0o600))
				runGit(t, dir, "add", ".bazelrc")
			}

			d := &Doctor{
				Envs: map[string]string{},
				LookPath: func(string) (string, error) {
					if tt.cliOnPATH {
						return "/usr/local/bin/bitrise-build-cache", nil
					}

					return "", errors.New("not found")
				},
			}

			res := d.bazelCredHelperCheck().Diagnose(context.Background())
			assert.Equal(t, tt.wantState, res.State, "detail=%s", res.Detail)
			assert.Contains(t, res.Detail, tt.wantDetail)
		})
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}
