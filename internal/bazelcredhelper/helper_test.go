//go:build unit

package bazelcredhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

func envResolver(t *testing.T, token string) Resolver {
	t.Helper()

	return NewResolver(map[string]string{
		auth.EnvAuthToken:   token,
		auth.EnvWorkspaceID: "ws-1",
	}, io.Discard)
}

func TestRun_EmitsBearerAuthorizationHeader(t *testing.T) {
	in := strings.NewReader(`{"uri":"https://bitrise-accelerate.services.bitrise.io"}`)
	out := &bytes.Buffer{}

	require.NoError(t, Run(t.Context(), in, out, envResolver(t, "test-token"), nil))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, []string{"Bearer test-token"}, resp.Headers["authorization"])
}

func TestRun_EmptyStdin_StillEmitsHeader(t *testing.T) {
	in := strings.NewReader("")
	out := &bytes.Buffer{}

	require.NoError(t, Run(t.Context(), in, out, envResolver(t, "test-token"), nil))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, []string{"Bearer test-token"}, resp.Headers["authorization"])
}

func TestRun_MalformedRequest_ReturnsError(t *testing.T) {
	in := strings.NewReader("not-json")
	out := &bytes.Buffer{}

	err := Run(t.Context(), in, out, envResolver(t, "test-token"), nil)
	require.Error(t, err)
	assert.Empty(t, out.Bytes(), "no partial output when the request is malformed")
}

func TestRun_UsesRawToken_NotGradleFormat(t *testing.T) {
	// Bazel's `--remote_header=authorization=` currently writes the bare token
	// (workspace ID travels via x-org-id). The helper must match that.
	in := strings.NewReader(`{"uri":"x"}`)
	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), in, out, envResolver(t, "raw-token"), nil))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, []string{"Bearer raw-token"}, resp.Headers["authorization"])
	assert.NotContains(t, resp.Headers["authorization"][0], "ws-1:", "workspace ID must not appear in the token")
}

// Bazel surfaces only the helper's stderr, so an unauthenticated build has this
// one line to work from — it has to name a command, not just what was missing.
func TestRun_NoCredentials_PointsAtDoctor(t *testing.T) {
	// HOME alone isn't enough: the resolver reads the real OS keychain, so on a
	// machine with stored credentials this would resolve and the test would pass
	// for the wrong reason.
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())

	out := &bytes.Buffer{}
	err := Run(t.Context(), strings.NewReader(`{"uri":"https://x.services.bitrise.io/"}`), out,
		NewResolver(map[string]string{}, io.Discard), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "doctor --fix --interactive")
	assert.Contains(t, err.Error(), "OS keychain", "the message should not blame env vars alone")
	assert.Empty(t, out.Bytes(), "no header is emitted without a credential")
	assert.NotContains(t, err.Error(), "\n", "Bazel prints this per failing RPC; keep it to one line")
}

func TestRun_EmitsExpires_FromCredentialExpiry(t *testing.T) {
	expiry := time.Now().Add(42 * time.Minute).Truncate(time.Second)
	resolve := func(context.Context) (Credential, error) {
		return Credential{Token: "tok", Expiry: expiry}, nil
	}

	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, resolve, nil))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))

	got, err := time.Parse(time.RFC3339, resp.Expires)
	require.NoError(t, err, "expires must be RFC 3339 per the EngFlow spec")
	assert.True(t, got.Equal(expiry), "got %s, want %s", got, expiry)
}

// Bazel only falls back to --credential_helper_cache_duration when the key is
// absent; a zero-time value would be a past expiry.
func TestRun_OmitsExpires_WhenExpiryUnknown(t *testing.T) {
	resolve := func(context.Context) (Credential, error) {
		return Credential{Token: "tok"}, nil
	}

	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, resolve, nil))

	var raw map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &raw))
	assert.NotContains(t, raw, "expires")
}

func TestRun_ResolverError_NoPartialOutput(t *testing.T) {
	resolve := func(context.Context) (Credential, error) {
		return Credential{}, errors.New("boom")
	}

	out := &bytes.Buffer{}
	err := Run(t.Context(), strings.NewReader(`{}`), out, resolve, nil)

	require.Error(t, err)
	assert.Empty(t, out.Bytes())
}

func TestRun_EmitsRepositoryURLHeader(t *testing.T) {
	in := strings.NewReader(`{"uri":"https://bitrise-accelerate.services.bitrise.io"}`)
	out := &bytes.Buffer{}

	repoURL := func(context.Context) string { return "https://github.com/org/repo.git" }
	require.NoError(t, Run(t.Context(), in, out, envResolver(t, "test-token"), repoURL))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, []string{"https://github.com/org/repo.git"}, resp.Headers["x-repository-url"])
}

func TestRun_NoRepositoryURL_OmitsHeader(t *testing.T) {
	in := strings.NewReader(`{"uri":"https://bitrise-accelerate.services.bitrise.io"}`)
	out := &bytes.Buffer{}

	repoURL := func(context.Context) string { return "" }
	require.NoError(t, Run(t.Context(), in, out, envResolver(t, "test-token"), repoURL))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.NotContains(t, resp.Headers, "x-repository-url")
}

func TestRun_ProjectModeOptInWithoutMarkerReturnsEmptyHeaders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeMachineConfig(t, home, `{"project_mode":"opt-in"}`)

	// A brand-new build dir without a marker file.
	buildDir := home + "/build-a"
	require.NoError(t, mustMkdir(buildDir))
	t.Chdir(buildDir)

	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, envResolver(t, "test-token"), nil))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Empty(t, resp.Headers, "opt-in without marker must emit empty headers so RPCs skip auth")
}

func TestRun_ProjectModeOptInWithMarkerStillAuths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeMachineConfig(t, home, `{"project_mode":"opt-in"}`)

	buildDir := home + "/build-b"
	require.NoError(t, mustMkdir(buildDir))
	require.NoError(t, mustWrite(buildDir+"/.bitrise-build-cache.json", "{}"))
	t.Chdir(buildDir)

	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, envResolver(t, "test-token"), nil))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, []string{"Bearer test-token"}, resp.Headers["authorization"])
}

func writeMachineConfig(t *testing.T, home, body string) {
	t.Helper()
	dir := home + "/.bitrise/build-cache"
	require.NoError(t, mustMkdir(dir))
	require.NoError(t, mustWrite(dir+"/config.json", body))
}

func mustMkdir(dir string) error {
	return osMkdirAll(dir, 0o755)
}

func mustWrite(path, body string) error {
	return osWriteFile(path, []byte(body), 0o644)
}

var (
	osMkdirAll   = os.MkdirAll
	osWriteFile  = os.WriteFile
)
