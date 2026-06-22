//go:build unit

package auth

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 4096 < typical kernel pipe buffer (16-64KB); the Gradle init script reads stdout then stderr sequentially, a larger stderr could deadlock.
const pipeBufferSafeBound = 4096

func TestAuthTokenCmd_stderrIsBoundedOnError(t *testing.T) {
	cmd := authTokenCmd

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	t.Setenv("BITRISE_BUILD_CACHE_AUTH_TOKEN", "")
	t.Setenv("BITRISE_BUILD_CACHE_WORKSPACE_ID", "")
	t.Setenv("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", "")

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Skip("dev machine has credentials configured; cannot exercise error path here")
	}

	require.Less(t, stderr.Len(), pipeBufferSafeBound,
		"auth token stderr must stay under %d bytes so the Gradle init script's sequential stdout+stderr drain can't deadlock", pipeBufferSafeBound)
	assert.NotEmpty(t, stderr.Bytes(), "error path must surface a one-line message on stderr")
}
