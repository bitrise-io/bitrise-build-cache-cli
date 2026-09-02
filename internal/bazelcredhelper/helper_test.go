//go:build unit

package bazelcredhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

// A credential expiring well after the cap is truncated to now + maxCacheHint,
// so a marker edit is picked up within a bounded window without per-RPC spawn.
func TestRun_EmitsExpires_CappedAtMaxCacheHint(t *testing.T) {
	expiry := time.Now().Add(42 * time.Minute).Truncate(time.Second)
	resolve := func(context.Context) (Credential, error) {
		return Credential{Token: "tok", Expiry: expiry}, nil
	}

	before := time.Now()
	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, resolve, nil))
	after := time.Now()

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))

	got, err := time.Parse(time.RFC3339, resp.Expires)
	require.NoError(t, err, "expires must be RFC 3339 per the EngFlow spec")
	assert.WithinRange(t, got, before.Add(maxCacheHint).Truncate(time.Second), after.Add(maxCacheHint))
	assert.True(t, got.Before(expiry), "cap must truncate a long-lived credential")
}

// A credential expiring inside the cap is honoured — the serve-stale path in
// resolver.go sets a short expiry deliberately, and stretching it would defeat
// the "repaired login gets picked up soon" contract.
func TestRun_EmitsExpires_UsesShorterCredentialExpiry(t *testing.T) {
	expiry := time.Now().Add(time.Minute).Truncate(time.Second)
	resolve := func(context.Context) (Credential, error) {
		return Credential{Token: "tok", Expiry: expiry}, nil
	}

	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, resolve, nil))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))

	got, err := time.Parse(time.RFC3339, resp.Expires)
	require.NoError(t, err)
	assert.True(t, got.Equal(expiry), "shorter credential expiry must win over the cap")
}

// A credExpiry already in the past takes the cap — otherwise Bazel would treat
// the response as an instant cache miss and spawn the helper per RPC.
func TestRun_EmitsExpires_PastExpiryTakesCap(t *testing.T) {
	past := time.Now().Add(-time.Minute).Truncate(time.Second)
	resolve := func(context.Context) (Credential, error) {
		return Credential{Token: "tok", Expiry: past}, nil
	}

	before := time.Now()
	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, resolve, nil))
	after := time.Now()

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))

	got, err := time.Parse(time.RFC3339, resp.Expires)
	require.NoError(t, err)
	assert.WithinRange(t, got, before.Add(maxCacheHint).Truncate(time.Second), after.Add(maxCacheHint))
}

// An unknown lifetime takes the cap so Bazel does not spawn the helper per RPC
// (a zero-time serialises as year 1 and reads as a past expiry).
func TestRun_EmitsExpires_WhenExpiryUnknown(t *testing.T) {
	resolve := func(context.Context) (Credential, error) {
		return Credential{Token: "tok"}, nil
	}

	before := time.Now()
	out := &bytes.Buffer{}
	require.NoError(t, Run(t.Context(), strings.NewReader(`{}`), out, resolve, nil))
	after := time.Now()

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))

	got, err := time.Parse(time.RFC3339, resp.Expires)
	require.NoError(t, err)
	assert.WithinRange(t, got, before.Add(maxCacheHint).Truncate(time.Second), after.Add(maxCacheHint))
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
