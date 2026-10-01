//go:build unit

package common_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

func TestOrgAllowedForAutoActivation(t *testing.T) {
	tests := []struct {
		name      string
		allowlist string
		workspace string
		want      bool
	}{
		{"unset list denies", "", "dbd227a0aeb70859", false},
		{"blank list denies", "  ,, ", "dbd227a0aeb70859", false},
		{"listed workspace", "dbd227a0aeb70859", "dbd227a0aeb70859", true},
		{"one of several, comma separated", "322a005426441b60,dbd227a0aeb70859", "dbd227a0aeb70859", true},
		{"one of several, whitespace separated", "322a005426441b60 dbd227a0aeb70859", "dbd227a0aeb70859", true},
		{"padding around entries", " 322a005426441b60 , dbd227a0aeb70859 ", "dbd227a0aeb70859", true},
		{"unlisted workspace", "322a005426441b60", "dbd227a0aeb70859", false},
		{"prefix of a listed slug is not a match", "dbd227a0aeb70859", "dbd227a0", false},
		{"slugs compare case-insensitively", "DBD227A0AEB70859", "dbd227a0aeb70859", true},
		{"star allows every workspace", "*", "dbd227a0aeb70859", true},
		{"star among others", "322a005426441b60,*", "dbd227a0aeb70859", true},
		{"no workspace denies even with star", "*", "", false},
		{"no workspace denies", "dbd227a0aeb70859", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, common.OrgAllowedForAutoActivation(tt.allowlist, tt.workspace))
		})
	}
}
