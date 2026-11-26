//go:build unit

package analytics_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

func TestPutInvocation_DebugLogOmitsEnvs(t *testing.T) {
	const secret = "s3cr3t-build-cache-token"
	bodies := make(chan []byte, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	var out bytes.Buffer
	c, err := analytics.NewClient(srv.URL, func() string { return "token" }, log.NewLogger(log.WithOutput(&out), log.WithDebugLog(true)))
	require.NoError(t, err)

	inv := analytics.NewInvocation(
		analytics.InvocationRunStats{InvocationID: "x"},
		auth.Credential{},
		common.CacheConfigMetadata{RedactedEnvs: map[string]string{"BITRISE_BUILD_CACHE_AUTH_TOKEN": secret}},
	)
	require.NoError(t, c.PutInvocation(*inv))

	assert.Contains(t, string(<-bodies), secret, "the PUT must still send the raw envs")
	assert.Equal(t, secret, inv.Envs["BITRISE_BUILD_CACHE_AUTH_TOKEN"], "the caller's envs must not be touched")
	// scripts/assert_cache_blob_stats.sh extracts the payload with grep -o '{"invocationId".*}'.
	assert.Contains(t, out.String(), `Payload: {"invocationId":"x"`)
	assert.NotContains(t, out.String(), secret)
}
