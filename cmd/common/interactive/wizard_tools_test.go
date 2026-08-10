//go:build unit

package interactive

import (
	"context"
	"testing"

	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSelectedTools_returnsAllFour(t *testing.T) {
	assert.Equal(t, []string{"gradle", "bazel", "xcode", "ccache"}, defaultSelectedTools())
}

func TestActivateReactNativeBasedOnSelection(t *testing.T) {
	cases := []struct {
		name        string
		tools       []string
		expectCalls int
	}{
		{"gradle+xcode → RN activated", []string{"gradle", "xcode"}, 1},
		{"only gradle → RN skipped", []string{"gradle"}, 0},
		{"only xcode → RN skipped", []string{"xcode"}, 0},
		{"gradle+xcode+ccache → RN activated", []string{"gradle", "xcode", "ccache"}, 1},
		{"bazel only → RN skipped", []string{"bazel"}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := activateReactNative
			t.Cleanup(func() { activateReactNative = original })

			var calls int
			activateReactNative = func(_ context.Context, _ log.Logger, _ bool) error {
				calls++

				return nil
			}

			require.NoError(t, activateReactNativeBasedOnSelection(context.Background(), log.NewLogger(), tc.tools))
			assert.Equal(t, tc.expectCalls, calls)
		})
	}
}

func TestDefaultSelectedTools_preselectsMultiSelectOptions(t *testing.T) {
	selected := defaultSelectedTools()
	field := huh.NewMultiSelect[string]().
		Options(
			huh.NewOption("Gradle", string(toolGradle)),
			huh.NewOption("Bazel", string(toolBazel)),
			huh.NewOption("Xcode", string(toolXcode)),
			huh.NewOption("ccache (C/C++)", string(toolCcache)),
		).
		Value(&selected)

	got, ok := field.GetValue().([]string)
	assert.True(t, ok, "GetValue should return []string")
	assert.ElementsMatch(t, []string{"gradle", "bazel", "xcode", "ccache"}, got)
}
