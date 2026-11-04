//go:build unit

package common

import (
	"context"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
)

func TestAutoActivationEnabled_AsksTheConfiguredWebsite(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"production by default", "", "https://app.bitrise.io"},
		{"the env override", "https://app-staging.bitrise.io/", "https://app-staging.bitrise.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(consts.EnvWebsiteBaseURL, tt.env)
			var got string
			orig := configcommon.AutoActivationChecker
			t.Cleanup(func() { configcommon.AutoActivationChecker = orig })
			configcommon.AutoActivationChecker = func(_ context.Context, baseURL string, _ auth.Credential, _ configcommon.AppIdentity, _ log.Logger) bool {
				got = baseURL

				return true
			}

			assert.True(t, AutoActivationEnabled(t.Context(), log.NewLogger(), auth.Credential{Token: "tok", WorkspaceID: "ws-1"}))
			assert.Equal(t, tt.want, got)
		})
	}
}
