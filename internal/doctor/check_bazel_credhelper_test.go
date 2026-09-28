//go:build unit

package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBazelCredHelperCheck(t *testing.T) {
	tests := []struct {
		name       string
		writeRc    bool
		cliOnPATH  bool
		wantState  State
		wantDetail string
	}{
		{name: "no pin, silent OK", writeRc: false, cliOnPATH: true, wantState: StateOK, wantDetail: "no repo-level"},
		{name: "pin + CLI present → warn", writeRc: true, cliOnPATH: true, wantState: StateWarn, wantDetail: "credential-helper pin"},
		{name: "pin + CLI missing → error", writeRc: true, cliOnPATH: false, wantState: StateError, wantDetail: "is not on PATH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			if tt.writeRc {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".bazelrc"),
					[]byte("build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n"), 0o600))
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
