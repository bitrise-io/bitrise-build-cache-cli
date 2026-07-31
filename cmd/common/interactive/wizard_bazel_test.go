//go:build unit

package interactive

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
)

// Without a CLIPath the bazelrc template falls back to a literal Bearer token:
// it writes the credential to disk, and an OAuth PAT baked in that way expires
// with nothing able to refresh it. The wizard used to omit it while the
// `activate bazel` command set it, so only the wizard was affected.
func TestInteractiveBazel_UsesTheCredentialHelper(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	params := bazelconfig.DefaultActivateBazelParams()
	params.CLIPath = exe

	inv, err := params.TemplateInventory(
		silentLogger(),
		map[string]string{"BITRISE_BUILD_CACHE_AUTH_TOKEN": "tok", "BITRISE_BUILD_CACHE_WORKSPACE_ID": "ws"},
		func(string, ...string) (string, error) { return "", nil },
		false,
	)
	require.NoError(t, err)

	assert.Equal(t, exe, inv.Common.CLIPath, "an empty CLIPath disables the helper and bakes the token in")
	assert.Empty(t, inv.Common.CIProvider, "the helper branch only applies off CI")
}
