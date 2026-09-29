//go:build unit

package bazelcredhelper

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
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

// writeBazelrc puts a generated block on disk with or without the warmup marker.
func writeBazelrc(t *testing.T, home string, lite bool) {
	t.Helper()

	body := "build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n"
	if lite {
		body = bazelconfig.LiteMarker + "\n" + body
	}
	require.NoError(t, os.WriteFile(paths.FromHome(home).BazelrcFile(), []byte(body), 0o600))
}

func runHelper(t *testing.T, out, warn *bytes.Buffer) error {
	t.Helper()

	return Run(t.Context(), strings.NewReader(`{}`), out, warn,
		NewResolver(map[string]string{}, io.Discard), nil, nil)
}

// After a warmup activation, a workspace without Build Cache reaches this on
// every build. Bazel must get a usable response, not a failing helper.
func TestRun_LiteActivationWithoutCredentialsEmitsEmptyHeaders(t *testing.T) {
	home := unconfiguredHome(t)
	writeBazelrc(t, home, true)

	out, warn := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, runHelper(t, out, warn))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Empty(t, resp.Headers, "no credential means no headers, but still exit 0")
	assert.Contains(t, warn.String(), "runs without the remote cache",
		"going uncached has to be said out loud somewhere")
}

// A token with no workspace id is "not configured" too, and used to go uncached
// with no message anywhere. The warning has to name the missing half.
func TestRun_LiteActivationWithHalfACredentialSaysWhichHalfIsMissing(t *testing.T) {
	home := unconfiguredHome(t)
	writeBazelrc(t, home, true)

	out, warn := &bytes.Buffer{}, &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, warn,
		NewResolver(map[string]string{auth.EnvAuthToken: "a-token"}, io.Discard), nil, nil))

	assert.Contains(t, warn.String(), auth.ErrWorkspaceIDNotProvided.Error())
}

// The marker lives in the bazelrc, so a bazelrc that configures the helper can
// never be on disk without the answer to "was there a credential when this was
// written?". A sidecar beside it could be, and a lost one turned every
// credential-less build on the VM into a hard failure.
func TestRun_LiteMarkerComesFromTheBazelrcItself(t *testing.T) {
	home := unconfiguredHome(t)
	writeBazelrc(t, home, true)
	// Nothing beside the bazelrc says anything about lite.
	require.NoError(t, bazelconfig.WriteSidecar(home, bazelconfig.Sidecar{BazelrcPath: "x"}))

	require.NoError(t, runHelper(t, &bytes.Buffer{}, &bytes.Buffer{}))
}

// A machine someone activated by hand had a credential at the time. Losing it is
// a misconfiguration, and staying quiet about it would strand the developer.
func TestRun_FullActivationWithoutCredentialsStillPointsAtDoctor(t *testing.T) {
	home := unconfiguredHome(t)
	writeBazelrc(t, home, false)

	err := runHelper(t, &bytes.Buffer{}, &bytes.Buffer{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "doctor --fix --interactive")
}

// No bazelrc at all is the pre-lite state, and it must keep the loud message.
func TestRun_NoBazelrcWithoutCredentialsStillPointsAtDoctor(t *testing.T) {
	unconfiguredHome(t)

	err := runHelper(t, &bytes.Buffer{}, &bytes.Buffer{})

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
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, io.Discard,
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
