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
		{"surrounding whitespace is dropped", map[string]string{consts.EnvWebsiteBaseURL: " http://localhost:3000 "}, "http://localhost:3000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, common.SelectWebsiteBaseURL(tt.envs))
		})
	}
}
