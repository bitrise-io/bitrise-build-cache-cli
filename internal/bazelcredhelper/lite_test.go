//go:build unit

package bazelcredhelper

import (
	"bytes"
	"context"
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

// writeBazelrc puts a generated block on disk with or without the lite marker.
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
		NewResolver(map[string]string{}, io.Discard), nil, nil, nil)
}

// After a lite activation, a workspace without Build Cache reaches this on
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
		NewResolver(map[string]string{auth.EnvAuthToken: "a-token"}, io.Discard), nil, nil, nil))

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

func ciEnvs() map[string]string {
	return map[string]string{
		"BITRISE_IO":                    "true",
		"BITRISE_BUILD_SLUG":            "build-slug-1",
		"BITRISE_APP_SLUG":              "app-slug-1",
		"BITRISE_TRIGGERED_WORKFLOW_ID": "primary",
	}
}

func runWithMetadata(t *testing.T, envs map[string]string) GetCredentialsResponse {
	t.Helper()

	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, io.Discard,
		envResolver(t, "test-token"), nil, NewMetadataResolver(envs), nil))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))

	return resp
}

// A lite bazelrc bakes no per-invocation metadata, so the helper has to supply
// it — that is what lets a warmed-up VM report the build it actually ran rather
// than the one that activated.
func TestRun_LiteEmitsPerInvocationMetadataHeaders(t *testing.T) {
	home := unconfiguredHome(t)
	writeBazelrc(t, home, true)

	resp := runWithMetadata(t, ciEnvs())

	assert.Equal(t, []string{"app-slug-1"}, resp.Headers["x-app-id"])
	assert.Equal(t, []string{"primary"}, resp.Headers["x-workflow-name"])
	assert.Equal(t, []string{"bitrise"}, resp.Headers["x-ci-provider"])
	// The cache and BES read the build id under different keys.
	assert.Equal(t, []string{"build-slug-1"}, resp.Headers["x-flare-build-id"])
	assert.Equal(t, []string{"build-slug-1"}, resp.Headers["x-build-id"])
	// From the credential, so it can never disagree with the token sent beside it.
	assert.Equal(t, []string{"ws-1"}, resp.Headers[orgIDHeader])
}

// A normal activation already baked this metadata into the bazelrc, so emitting
// it here too would put two values in the same key. Deferring metadata is a lite
// behaviour only — it must not change what every existing user of the CLI gets.
func TestRun_NonLiteEmitsNoMetadataHeaders(t *testing.T) {
	home := unconfiguredHome(t)
	writeBazelrc(t, home, false)

	resp := runWithMetadata(t, ciEnvs())

	require.NotEmpty(t, resp.Headers["authorization"], "auth is still the helper's job on both branches")
	for _, header := range []string{"x-app-id", "x-workflow-name", "x-ci-provider", "x-flare-build-id", "x-build-id", orgIDHeader} {
		assert.NotContains(t, resp.Headers, header, "the bazelrc already carries this one")
	}
}

// Off CI there is no build to describe, and empty headers would be noise.
func TestNewMetadataResolver_OmitsWhatItCannotResolve(t *testing.T) {
	// With no CI detected the build-user fallback goes through ResolveUsername,
	// which reads the credential stores — including the OS keychain.
	unconfiguredHome(t)

	headers := NewMetadataResolver(map[string]string{auth.EnvAuthToken: "t"})()

	assert.NotContains(t, headers, appIDHeader)
	assert.NotContains(t, headers, cacheBuildHeader)
	assert.NotContains(t, headers, ciProviderHeader)
}

// A credential resolves but the workspace has no Build Cache. Withholding the
// header is the same stand-down as having no credential: Bazel sends no auth
// and the build runs uncached, rather than the helper failing the RPC.
func TestRun_LiteWithoutEntitlementWithholdsTheCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBazelrc(t, home, true)

	out := &bytes.Buffer{}
	skip := func(context.Context, Credential) bool { return true }
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, io.Discard,
		func(context.Context) (Credential, error) {
			return Credential{Token: "tok", WorkspaceID: "ws"}, nil
		}, nil, nil, skip))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Empty(t, resp.Headers, "an unentitled workspace must get no authorization header")
}

// The gate must not fire for a full activation: it already asked before writing
// anything, and a second check here would gate every existing Bazel user.
func TestRun_NonLiteIgnoresTheEntitlementGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBazelrc(t, home, false)

	out := &bytes.Buffer{}
	skip := func(context.Context, Credential) bool { return true }
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, io.Discard,
		func(context.Context) (Credential, error) {
			return Credential{Token: "tok", WorkspaceID: "ws"}, nil
		}, nil, nil, skip))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, []string{"Bearer tok"}, resp.Headers["authorization"])
}
