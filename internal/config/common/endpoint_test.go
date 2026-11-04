//go:build unit

package common_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
)

func TestSelectWebsiteBaseURL(t *testing.T) {
	tests := []struct {
		name string
		envs map[string]string
		want string
	}{
		{"production by default", map[string]string{}, "https://app.bitrise.io"},
		{"an empty value is the default", map[string]string{consts.EnvWebsiteBaseURL: "  "}, "https://app.bitrise.io"},
		{"the env wins", map[string]string{consts.EnvWebsiteBaseURL: "https://app-staging.bitrise.io"}, "https://app-staging.bitrise.io"},
		{"a trailing slash is dropped", map[string]string{consts.EnvWebsiteBaseURL: "https://app-staging.bitrise.io/"}, "https://app-staging.bitrise.io"},
		{"surrounding whitespace is dropped", map[string]string{consts.EnvWebsiteBaseURL: " https://app-staging.bitrise.io "}, "https://app-staging.bitrise.io"},
		{"another host is ignored", map[string]string{consts.EnvWebsiteBaseURL: "https://evil.example"}, "https://app.bitrise.io"},
		{"plain http is ignored", map[string]string{consts.EnvWebsiteBaseURL: "http://app.bitrise.io"}, "https://app.bitrise.io"},
		{"a lookalike host is ignored", map[string]string{consts.EnvWebsiteBaseURL: "https://app.bitrise.io.evil.example"}, "https://app.bitrise.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, common.SelectWebsiteBaseURL(tt.envs))
		})
	}
}

func TestSelectCacheEndpointURL_EnvOverride(t *testing.T) {
	const key = "BITRISE_BUILD_CACHE_ENDPOINT"

	tests := []struct {
		name string
		env  string
		want string
	}{
		{"a Bitrise grpcs host is used", "grpcs://cache.bitrise.dev:443", "grpcs://cache.bitrise.dev:443"},
		{"another host is ignored", "grpcs://evil.example", consts.BitriseAccelerate},
		{"plaintext grpc is ignored", "grpc://cache.bitrise.io", consts.BitriseAccelerate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, common.SelectCacheEndpointURL("", map[string]string{key: tt.env}))
		})
	}

	assert.Equal(t, "grpc://localhost:1", common.SelectCacheEndpointURL("grpc://localhost:1", map[string]string{key: "grpcs://evil.example"}),
		"an explicit value is the caller's own choice")
}

func TestSelectRBEEndpointURL_EnvOverride(t *testing.T) {
	const key = "BITRISE_RBE_ENDPOINT"

	assert.Equal(t, "rbe.bitrise.io:443", common.SelectRBEEndpointURL("", map[string]string{key: "rbe.bitrise.io:443"}))
	assert.Equal(t, "grpcs://rbe.bitrise.io", common.SelectRBEEndpointURL("", map[string]string{key: "grpcs://rbe.bitrise.io"}))
	assert.Equal(t, consts.BitriseAccelerate, common.SelectRBEEndpointURL("", map[string]string{key: "rbe.evil.example:443"}))
}
