//nolint:maintidx
package gradleconfig

import (
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

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

func liteParams() ActivateGradleParams {
	params := DefaultActivateGradleParams()
	params.Cache.Enabled = true
	params.Analytics.Enabled = true
	params.Lite = true

	return params
}

// Warmup runs before any credential exists, so activation has to produce the
// wiring anyway — the plugins fetch the token themselves mid-build.
func TestTemplateInventory_LiteSucceedsWithoutACredential(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	inventory, err := liteParams().TemplateInventory(
		t.Context(), liteTestLogger(), map[string]string{}, false, nil, utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.Empty(t, inventory.Common.AuthToken, "lite must not bake a token")
	assert.Equal(t, UsageLevelEnabled, inventory.Cache.Usage)
	assert.Equal(t, UsageLevelEnabled, inventory.Analytics.Usage)
}

// providerName used to be emitted even when empty, which pinned every warmed-up
// VM's analytics to "local" — the plugin's own CI detection never got to run.
func TestGenerateInitGradle_LiteOmitsEmptyProviderName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	inventory, err := liteParams().TemplateInventory(
		t.Context(), liteTestLogger(), map[string]string{}, false, nil, utils.DefaultOsProxy{})
	require.NoError(t, err)

	got, err := inventory.GenerateInitGradle(GradleTemplateProxy())
	require.NoError(t, err)

	assert.NotContains(t, got, "providerName.set(")
	assert.NotContains(t, got, `appSlug.set("")`)
}

// The init script is a configuration-cache input, compared by content hash. Two
// warmups on identical VMs have to produce identical bytes or every build
// re-configures from scratch.
func TestGenerateInitGradle_LiteOutputIsStableAcrossRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	render := func() string {
		inventory, err := liteParams().TemplateInventory(
			t.Context(), liteTestLogger(), map[string]string{}, false, nil, utils.DefaultOsProxy{})
		require.NoError(t, err)

		got, err := inventory.GenerateInitGradle(GradleTemplateProxy())
		require.NoError(t, err)

		return got
	}

	first := render()

	assert.Equal(t, first, render())
	assert.NotContains(t, first, `authToken.set("`, "a baked token would change every build")
}
