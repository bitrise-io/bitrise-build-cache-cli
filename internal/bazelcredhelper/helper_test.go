//go:build unit

package bazelcredhelper

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
