//go:build unit

package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

func TestBazelCLIBinOnPATHCheck(t *testing.T) {
	tests := []struct {
		name       string
		activated  map[toolconfig.Tool]bool
		cliOnPATH  bool
		wantState  State
		wantDetail string
	}{
		{
			name:       "bazel not activated → skipped OK",
			activated:  map[toolconfig.Tool]bool{},
			cliOnPATH:  false,
			wantState:  StateOK,
			wantDetail: "skipped",
		},
		{
			name:       "bazel activated + CLI on PATH → OK",
			activated:  map[toolconfig.Tool]bool{toolconfig.Bazel: true},
			cliOnPATH:  true,
			wantState:  StateOK,
			wantDetail: "resolves on $PATH",
		},
		{
			name:       "bazel activated + CLI missing → warn",
			activated:  map[toolconfig.Tool]bool{toolconfig.Bazel: true},
			cliOnPATH:  false,
			wantState:  StateWarn,
			wantDetail: "not on $PATH but bazel was activated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Doctor{
				ActivatedTools: func() map[toolconfig.Tool]bool { return tt.activated },
				LookPath: func(string) (string, error) {
					if tt.cliOnPATH {
						return "/usr/local/bin/bitrise-build-cache", nil
					}

					return "", errors.New("not found")
				},
			}

			res := d.bazelCLIBinOnPATHCheck().Diagnose(context.Background())
			assert.Equal(t, tt.wantState, res.State, "detail=%s", res.Detail)
			assert.Contains(t, res.Detail, tt.wantDetail)
		})
	}
}
