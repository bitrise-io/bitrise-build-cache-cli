//go:build unit

package xcode

import (
	"io"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

func TestXcodebuildRunner_analyticsClientsFollowTheServicesDomain(t *testing.T) {
	for _, tc := range []struct {
		domain, wantXcode, wantMultiplatform string
	}{
		{"", "https://xcode-analytics.services.bitrise.io", "https://multiplatform-analytics.services.bitrise.io"},
		{"bitrise.dev", "https://xcode-analytics.services.bitrise.dev", "https://multiplatform-analytics.services.bitrise.dev"},
	} {
		t.Run("domain="+tc.domain, func(t *testing.T) {
			t.Setenv("BITRISE_BUILD_CACHE_SERVICES_DOMAIN", tc.domain)
			runner := &XcodebuildRunner{Logger: log.NewLogger(log.WithOutput(io.Discard))}

			invocationAPI, err := runner.resolveInvocationAPI()
			require.NoError(t, err)
			xcodeClient, ok := invocationAPI.(*analytics.Client)
			require.True(t, ok)
			assert.Equal(t, tc.wantXcode, xcodeClient.BaseURL())

			relationAPI, err := runner.resolveRelationAPI()
			require.NoError(t, err)
			relationClient, ok := relationAPI.(*multiplatform.Client)
			require.True(t, ok)
			assert.Equal(t, tc.wantMultiplatform, relationClient.BaseURL())
		})
	}
}
