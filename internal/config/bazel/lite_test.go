//go:build unit

package bazelconfig

import (
	"os"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func writeBazelrc(t *testing.T, home, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(paths.FromHome(home).BazelrcFile(), []byte(body), 0o600))
}

func liteTestLogger() log.Logger {
	l := &mocks.Logger{}
	for _, m := range []string{"Infof", "Debugf", "Errorf", "Warnf"} {
		l.On(m, mock.Anything).Return()
		l.On(m, mock.Anything, mock.Anything).Return()
	}

	return l
}

// isolate keeps the resolver away from anything real: HOME alone is not enough,
// because the stores also read the OS keychain.
func isolate(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
}

// populatedCIEnv is everything the monolith injects into a build. Lite has to
// exclude all of it, and the exclusion must not depend on the environment
// happening to be empty — a warmup that runs inside a build sees exactly this.
func populatedCIEnv() map[string]string {
	return map[string]string{
		"BITRISE_IO":                    "true",
		"BITRISE_BUILD_SLUG":            "build-slug-abcdef12",
		"BITRISE_APP_SLUG":              "app-slug-abcdef12",
		"BITRISE_TRIGGERED_WORKFLOW_ID": "primary",
		auth.EnvAuthToken:               "auth-token-abcdef12",
		auth.EnvWorkspaceID:             "workspace-id-abcdef12",
	}
}

func liteBazelParams() ActivateBazelParams {
	params := DefaultActivateBazelParams()
	params.Lite = true
	// Warmup installs the CLI at a stable path; without one lite refuses.
	params.CLIPath = "/usr/local/bin/bitrise-build-cache"

	return params
}

func renderLiteBazelrc(t *testing.T, params ActivateBazelParams, envs map[string]string) (TemplateInventory, string) {
	t.Helper()

	inventory, err := params.TemplateInventory(
		t.Context(), liteTestLogger(), envs,
		func(string, ...string) (string, error) { return "", nil },
		false)
	require.NoError(t, err)

	got, err := inventory.GenerateBazelrc(utils.DefaultTemplateProxy())
	require.NoError(t, err)

	return inventory, got
}

// The exclusion has to be structural. Feeding an empty environment proves
// nothing: such a test would pass with Lite ignored entirely.
func TestGenerateBazelrc_LiteExcludesAFullyPopulatedCIEnvironment(t *testing.T) {
	isolate(t)

	envs := populatedCIEnv()
	inventory, got := renderLiteBazelrc(t, liteBazelParams(), envs)

	for name, value := range envs {
		// "true" and the like are too generic to grep for.
		if len(value) < 8 {
			continue
		}
		assert.NotContains(t, got, value, "%s leaked into the bazelrc", name)
	}

	assert.Empty(t, inventory.Common.AuthToken)
	assert.Empty(t, inventory.Common.WorkspaceID)
	assert.Empty(t, inventory.Common.AppSlug)
	assert.Empty(t, inventory.Common.CIProvider)
	assert.Empty(t, inventory.Common.BuildID)
	assert.Empty(t, inventory.Common.WorkflowName)
	assert.Empty(t, inventory.Common.RepoURL)

	// The static wiring is still written; only the identities are withheld.
	assert.Contains(t, got, "build --remote_cache=")
	assert.Contains(t, got, "build --bes_backend=")
	assert.Contains(t, got, "build --credential_helper=")
}

// The marker lives in the bazelrc so it and the config it describes are one
// write. `get` reads it to tell an unconfigured machine from a broken one.
func TestGenerateBazelrc_LiteEmitsTheWarmupMarker(t *testing.T) {
	isolate(t)

	_, lite := renderLiteBazelrc(t, liteBazelParams(), populatedCIEnv())

	full := liteBazelParams()
	full.Lite = false
	_, notLite := renderLiteBazelrc(t, full, populatedCIEnv())

	assert.Contains(t, lite, LiteMarker)
	assert.NotContains(t, notLite, LiteMarker)
}

func TestIsLiteBazelrc(t *testing.T) {
	isolate(t)
	home := t.TempDir()

	assert.False(t, IsLiteBazelrc(home), "no bazelrc is not a warmup")

	_, body := renderLiteBazelrc(t, liteBazelParams(), populatedCIEnv())
	writeBazelrc(t, home, body)
	assert.True(t, IsLiteBazelrc(home))

	full := liteBazelParams()
	full.Lite = false
	_, fullBody := renderLiteBazelrc(t, full, populatedCIEnv())
	writeBazelrc(t, home, fullBody)
	assert.False(t, IsLiteBazelrc(home))
}

// Without a reachable CLI there is no helper, and lite has no token to fall
// back on — the bazelrc would be one that can never authenticate.
func TestTemplateInventory_LiteRefusesWhenTheCLIIsUnreachable(t *testing.T) {
	isolate(t)
	t.Setenv("PATH", t.TempDir())

	params := liteBazelParams()
	params.CLIPath = ""

	_, err := params.TemplateInventory(
		t.Context(), liteTestLogger(), map[string]string{},
		func(string, ...string) (string, error) { return "", nil }, false)

	require.ErrorIs(t, err, errLiteNeedsCredentialHelper)
}
