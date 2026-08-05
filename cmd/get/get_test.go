//go:build unit

package get

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform" // registers the config-file credential readers
)

// A single stray log line on stdout breaks every build using the helper.
func TestGetCmd_StdoutIsOnlyTheJSONResponse(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(configcommon.EnvAuthToken, "test-token")
	t.Setenv(configcommon.EnvWorkspaceID, "ws-1")

	var stdout, stderr bytes.Buffer
	getCmd.SetContext(t.Context())
	getCmd.SetIn(strings.NewReader(`{"uri":"https://bitrise-accelerate.services.bitrise.io"}`))
	getCmd.SetOut(&stdout)
	getCmd.SetErr(&stderr)

	require.NoError(t, getCmd.RunE(getCmd, nil))

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	require.Len(t, lines, 1, "stdout must carry the JSON response and nothing else, got:\n%s", stdout.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &resp))
	assert.Contains(t, resp, "headers")
	assert.Empty(t, stderr.String())
}
