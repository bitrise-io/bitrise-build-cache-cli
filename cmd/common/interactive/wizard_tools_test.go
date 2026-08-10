//go:build unit

package interactive

import (
	"testing"

	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSelectedTools_returnsAllFour(t *testing.T) {
	assert.Equal(t, []string{"gradle", "bazel", "xcode", "ccache"}, defaultSelectedTools())
}

func TestFinalizeReactNativeIfBothSelected(t *testing.T) {
	cases := []struct {
		name        string
		tools       []string
		expectCalls int
	}{
		{"gradle+xcode → markers written", []string{"gradle", "xcode"}, 1},
		{"only gradle → markers skipped", []string{"gradle"}, 0},
		{"only xcode → markers skipped", []string{"xcode"}, 0},
		{"gradle+xcode+ccache → markers written", []string{"gradle", "xcode", "ccache"}, 1},
		{"bazel only → markers skipped", []string{"bazel"}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := saveReactNativeMarkersFn
			t.Cleanup(func() { saveReactNativeMarkersFn = original })

			var calls int
			saveReactNativeMarkersFn = func(_ log.Logger, _ bool) error {
				calls++

				return nil
			}

			require.NoError(t, finalizeReactNativeIfBothSelected(log.NewLogger(), tc.tools))
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
