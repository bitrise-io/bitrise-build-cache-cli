//go:build unit

package gradleconfig

import (
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func liteTestLogger() log.Logger {
	l := &mocks.Logger{}
	for _, m := range []string{"Infof", "Debugf", "Errorf", "Warnf"} {
		l.On(m, mock.Anything).Return()
		l.On(m, mock.Anything, mock.Anything).Return()
	}

	return l
}

// isolate keeps the resolver away from anything real: HOME alone is not enough,
// because the stores also read the OS keychain, which on a developer machine
// holds a live Bitrise credential.
func isolate(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
}

func liteParams() ActivateGradleParams {
	params := DefaultActivateGradleParams()
	params.Cache.Enabled = true
	params.Analytics.Enabled = true
	params.TestDistro.Enabled = true
	params.TestDistro.PoolName = "a-pool"
	params.Lite = true
	// Warmup installs the CLI at a stable path; without one lite refuses to write
	// a config whose token resolver can never run.
	params.CLIPath = "/usr/local/bin/bitrise-build-cache"

	return params
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

func renderLite(t *testing.T, envs map[string]string) (TemplateInventory, string) {
	t.Helper()

	inventory, err := liteParams().TemplateInventory(
		t.Context(), liteTestLogger(), envs, false, nil, utils.DefaultOsProxy{})
	require.NoError(t, err)

	got, err := inventory.GenerateInitGradle(GradleTemplateProxy())
	require.NoError(t, err)

	return inventory, got
}

// Warmup runs before any credential exists, so activation has to produce the
// wiring anyway — the plugins fetch the token themselves mid-build.
func TestTemplateInventory_LiteSucceedsWithoutACredential(t *testing.T) {
	isolate(t)

	inventory, _ := renderLite(t, map[string]string{})

	assert.Empty(t, inventory.Common.AuthToken, "lite must not bake a token")
	assert.Equal(t, UsageLevelEnabled, inventory.Cache.Usage)
	assert.Equal(t, UsageLevelEnabled, inventory.Analytics.Usage)
}

// The exclusion has to be structural. Feeding an empty environment proves
// nothing: those tests would pass with Lite ignored entirely.
func TestGenerateInitGradle_LiteExcludesAFullyPopulatedCIEnvironment(t *testing.T) {
	isolate(t)

	envs := populatedCIEnv()
	inventory, got := renderLite(t, envs)

	for name, value := range envs {
		// "true" and the like are too generic to grep for; the identifying
		// values are the long ones.
		if len(value) < 8 {
			continue
		}
		assert.NotContains(t, got, value, "%s leaked into the init script", name)
	}
	assert.Empty(t, inventory.Common.AuthToken)
	assert.Empty(t, inventory.Common.AppSlug)
	assert.Empty(t, inventory.Common.CIProvider)
	// The CLI-resolving branch is the one that has no token in it.
	assert.Contains(t, got, "BitriseAuthTokenSource")
}

// providerName and appSlug used to be emitted even when empty, which pinned
// every warmed-up VM's analytics to "local" — the plugin's own CI detection
// never got to run.
func TestGenerateInitGradle_LiteOmitsEmptyMetadataSetters(t *testing.T) {
	isolate(t)

	_, got := renderLite(t, populatedCIEnv())

	assert.NotContains(t, got, "providerName.set(")
	assert.NotContains(t, got, `appSlug.set("")`)
	// Non-vacuous: the TestDistro block that carried the second one is rendered.
	assert.Contains(t, got, "io.bitrise.gradle.rbe.RBEPlugin")
}

// On a credential-less VM the token resolver fails at every configuration, and
// a printed error there is spam on every build of every workspace without
// Build Cache.
func TestGenerateInitGradle_LiteDoesNotPrintAuthErrors(t *testing.T) {
	isolate(t)

	_, lite := renderLite(t, map[string]string{})

	full := liteParams()
	full.Lite = false
	// No CI provider, so this renders the same CLI-resolving branch lite does.
	inventory, err := full.TemplateInventory(
		t.Context(), liteTestLogger(),
		map[string]string{auth.EnvAuthToken: "t", auth.EnvWorkspaceID: "w"},
		false, nil, utils.DefaultOsProxy{})
	require.NoError(t, err)
	fullRendered, err := inventory.GenerateInitGradle(GradleTemplateProxy())
	require.NoError(t, err)

	assert.NotContains(t, lite, "System.err.println(\"bitrise-build-cache auth token exited")
	assert.Contains(t, fullRendered, "System.err.println(\"bitrise-build-cache auth token exited",
		"a hand-activated machine still reports a broken token")
}

// Nothing can resolve the token mid-build if the CLI is not reachable, and
// lite bakes none. Better a failed warmup than a config that can never work.
func TestTemplateInventory_LiteRefusesWhenTheCLIIsUnreachable(t *testing.T) {
	isolate(t)
	t.Setenv("PATH", t.TempDir())

	params := liteParams()
	params.CLIPath = ""

	_, err := params.TemplateInventory(
		t.Context(), liteTestLogger(), map[string]string{}, false, nil, utils.DefaultOsProxy{})

	require.ErrorIs(t, err, errLiteNeedsReachableCLI)
}

// The init script is a configuration-cache input, compared by content hash. Two
// warmups on identical VMs have to produce identical bytes or every build
// re-configures from scratch.
func TestGenerateInitGradle_LiteOutputIsStableAcrossRuns(t *testing.T) {
	isolate(t)

	_, first := renderLite(t, map[string]string{})
	_, second := renderLite(t, map[string]string{})

	assert.Equal(t, first, second)
	assert.NotContains(t, first, `authToken.set("`, "a baked token would change every build")
}

// Opt-in renders a scope-check ValueSource that shells out to the CLI on every
// configuration, to answer a question that has no meaning on a VM about to run
// an arbitrary build. A mode left on the machine must not leak into lite.
func TestTemplateInventory_LiteIgnoresAPersistedOptInProjectMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	keyring.MockInit()

	p, err := paths.Default()
	require.NoError(t, err)
	require.NoError(t, machineconfig.Write(
		machineconfig.Config{ProjectMode: machineconfig.ModeOptIn}, utils.DefaultOsProxy{}, p))

	inventory, err := liteParams().TemplateInventory(
		t.Context(), liteTestLogger(), map[string]string{}, false, nil, utils.DefaultOsProxy{})
	require.NoError(t, err)

	assert.Equal(t, string(machineconfig.ModeAlways), inventory.Common.ProjectMode)

	got, err := inventory.GenerateInitGradle(GradleTemplateProxy())
	require.NoError(t, err)
	assert.NotContains(t, got, "BitriseProjectScopeSource", "the scope-check must not render under lite")
}
