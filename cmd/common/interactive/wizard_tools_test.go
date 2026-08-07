//go:build unit

package interactive

import (
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
)

func TestDefaultSelectedTools_returnsAllFour(t *testing.T) {
	assert.Equal(t, []string{"gradle", "bazel", "xcode", "ccache"}, defaultSelectedTools())
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
