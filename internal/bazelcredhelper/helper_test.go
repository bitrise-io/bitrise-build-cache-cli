//go:build unit

package bazelcredhelper

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

func TestRun_EmitsBearerAuthorizationHeader(t *testing.T) {
	envs := map[string]string{
		configcommon.EnvAuthToken:   "test-token",
		configcommon.EnvWorkspaceID: "ws-1",
	}

	in := strings.NewReader(`{"uri":"https://bitrise-accelerate.services.bitrise.io"}`)
	out := &bytes.Buffer{}

	require.NoError(t, Run(in, out, envs))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, []string{"Bearer test-token"}, resp.Headers["authorization"])
}

func TestRun_EmptyStdin_StillEmitsHeader(t *testing.T) {
	envs := map[string]string{
		configcommon.EnvAuthToken:   "test-token",
		configcommon.EnvWorkspaceID: "ws-1",
	}

	in := strings.NewReader("")
	out := &bytes.Buffer{}

	require.NoError(t, Run(in, out, envs))

	var resp GetCredentialsResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, []string{"Bearer test-token"}, resp.Headers["authorization"])
}

func TestRun_MalformedRequest_ReturnsError(t *testing.T) {
	envs := map[string]string{
		configcommon.EnvAuthToken:   "test-token",
		configcommon.EnvWorkspaceID: "ws-1",
	}

	in := strings.NewReader("not-json")
	out := &bytes.Buffer{}

	err := Run(in, out, envs)
	require.Error(t, err)
	assert.Empty(t, out.Bytes(), "no partial output when the request is malformed")
}

func TestRun_UsesRawToken_NotGradleFormat(t *testing.T) {
	// Bazel's `--remote_header=authorization=` currently writes the bare token
	// (workspace ID travels via x-org-id). The helper must match that.
	envs := map[string]string{
		configcommon.EnvAuthToken:   "raw-token",
		configcommon.EnvWorkspaceID: "ws-1",
	}

	in := strings.NewReader(`{"uri":"x"}`)
	out := &bytes.Buffer{}
	require.NoError(t, Run(in, out, envs))

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
	err := Run(strings.NewReader(`{"uri":"https://x.services.bitrise.io/"}`), out, map[string]string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "doctor --fix --interactive")
	assert.Contains(t, err.Error(), "OS keychain", "the message should not blame env vars alone")
	assert.Empty(t, out.Bytes(), "no header is emitted without a credential")
	assert.NotContains(t, err.Error(), "\n", "Bazel prints this per failing RPC; keep it to one line")
}
