//go:build unit

package bazelcredhelper

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
)

// unconfiguredHome isolates the resolver from anything real: HOME alone is not
// enough, because the stores also read the OS keychain.
func unconfiguredHome(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)

	return home
}

func runHelper(t *testing.T, out *bytes.Buffer) error {
	t.Helper()

	return Run(t.Context(), strings.NewReader(`{}`), out,
		NewResolver(map[string]string{}, io.Discard), nil, nil)
}

// After a warmup activation, a workspace without Build Cache reaches this on
// every build. Bazel must get a usable response, not a failing helper.
func TestRun_LiteActivationWithoutCredentialsEmitsEmptyHeaders(t *testing.T) {
	home := unconfiguredHome(t)
	require.NoError(t, bazelconfig.WriteSidecar(home, bazelconfig.Sidecar{Lite: true}))

	out := &bytes.Buffer{}
	require.NoError(t, runHelper(t, out))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Empty(t, resp.Headers, "no credential means no headers, but still exit 0")
}

// A machine someone activated by hand had a credential at the time. Losing it is
// a misconfiguration, and staying quiet about it would strand the developer.
func TestRun_FullActivationWithoutCredentialsStillPointsAtDoctor(t *testing.T) {
	home := unconfiguredHome(t)
	require.NoError(t, bazelconfig.WriteSidecar(home, bazelconfig.Sidecar{Lite: false}))

	err := runHelper(t, &bytes.Buffer{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "doctor --fix --interactive")
}

// No sidecar at all is the pre-lite state, and it must keep the loud message.
func TestRun_NoSidecarWithoutCredentialsStillPointsAtDoctor(t *testing.T) {
	unconfiguredHome(t)

	err := runHelper(t, &bytes.Buffer{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "doctor --fix --interactive")
}

// The headers the bazelrc used to bake now travel per invocation, so a warmed-up
// VM reports the build it actually ran rather than the one that activated.
func TestRun_EmitsPerInvocationMetadataHeaders(t *testing.T) {
	envs := map[string]string{
		"BITRISE_IO":                    "true",
		"BITRISE_BUILD_SLUG":            "build-slug-1",
		"BITRISE_APP_SLUG":              "app-slug-1",
		"BITRISE_TRIGGERED_WORKFLOW_ID": "primary",
	}

	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out,
		envResolver(t, "test-token"), nil, NewMetadataResolver(envs)))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))

	assert.Equal(t, []string{"app-slug-1"}, resp.Headers["x-app-id"])
	assert.Equal(t, []string{"primary"}, resp.Headers["x-workflow-name"])
	assert.Equal(t, []string{"bitrise"}, resp.Headers["x-ci-provider"])
	// The cache and BES read the build id under different keys.
	assert.Equal(t, []string{"build-slug-1"}, resp.Headers["x-flare-build-id"])
	assert.Equal(t, []string{"build-slug-1"}, resp.Headers["x-build-id"])
	// From the credential, so it can never disagree with the token sent beside it.
	assert.Equal(t, []string{"ws-1"}, resp.Headers[orgIDHeader])
}

// Off CI there is no build to describe, and empty headers would be noise.
func TestNewMetadataResolver_OmitsWhatItCannotResolve(t *testing.T) {
	headers := NewMetadataResolver(map[string]string{auth.EnvAuthToken: "t"})()

	assert.NotContains(t, headers, appIDHeader)
	assert.NotContains(t, headers, cacheBuildHeader)
	assert.NotContains(t, headers, ciProviderHeader)
}
