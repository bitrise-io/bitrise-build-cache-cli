//go:build unit

package trustedurl_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/trustedurl"
)

func TestAllowed(t *testing.T) {
	tests := []struct {
		raw     string
		schemes []string
		want    bool
	}{
		{"https://app.bitrise.io", []string{"https"}, true},
		{"https://app-staging.bitrise.io/", []string{"https"}, true},
		{"https://bitrise.io", []string{"https"}, true},
		{"https://foo.bitrise.dev:8443/x", []string{"https"}, true},
		{"HTTPS://APP.BITRISE.IO", []string{"https"}, true},
		{"https://App.Bitrise.IO", []string{"https"}, true},
		{" https://app.bitrise.io ", []string{"https"}, true},
		{"grpcs://bitrise-accelerate.services.bitrise.io", []string{"grpcs"}, true},
		{"http://app.bitrise.io", []string{"https"}, false},
		{"grpc://bitrise-accelerate.services.bitrise.io", []string{"grpcs"}, false},
		{"https://evil.example", []string{"https"}, false},
		{"https://evilbitrise.io", []string{"https"}, false},
		{"https://bitrise.io.evil.example", []string{"https"}, false},
		{"https://app.bitrise.io@evil.example", []string{"https"}, false},
		{"https://evil.example@app.bitrise.io", []string{"https"}, false},
		{"https://evil.example/?u=app.bitrise.io", []string{"https"}, false},
		{"http://localhost:3000", []string{"https"}, false},
		{"app.bitrise.io:443", []string{"grpcs", ""}, true},
		{"app.bitrise.io:443", []string{"grpcs"}, false},
		{"evil.example:443", []string{"grpcs", ""}, false},
		{"", []string{"https"}, false},
		{"://", []string{"https"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			assert.Equal(t, tt.want, trustedurl.Allowed(tt.raw, tt.schemes...))
		})
	}
}

func TestFromEnv(t *testing.T) {
	envs := map[string]string{"GOOD": " https://app.bitrise.io ", "BAD": "https://evil.example"}

	assert.Equal(t, "https://app.bitrise.io", trustedurl.FromEnv(envs, "GOOD", "https"))
	assert.Empty(t, trustedurl.FromEnv(envs, "BAD", "https"))
	assert.Empty(t, trustedurl.FromEnv(envs, "MISSING", "https"))
}
