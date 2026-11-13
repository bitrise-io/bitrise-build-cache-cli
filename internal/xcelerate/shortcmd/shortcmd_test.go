//go:build unit

package shortcmd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/shortcmd"
)

func TestRender(t *testing.T) {
	cases := []struct {
		name          string
		action        string
		scheme        string
		testPlan      string
		configuration string
		want          string
	}{
		{"all four", "build", "MyScheme", "MyPlan", "Debug", "build [MyScheme / MyPlan / Debug]"},
		{"action + scheme", "build", "MyScheme", "", "", "build [MyScheme]"},
		{"action + scheme + config", "build", "MyScheme", "", "Debug", "build [MyScheme / Debug]"},
		{"action + config only", "build", "", "", "Debug", "build [Debug]"},
		{"action only", "build", "", "", "", "build"},
		{"bracket only", "", "MyScheme", "", "Debug", "[MyScheme / Debug]"},
		{"empty", "", "", "", "", ""},
		{"test + plan", "test", "MyScheme", "Smoke", "", "test [MyScheme / Smoke]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shortcmd.Render(c.action, c.scheme, c.testPlan, c.configuration)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestSuffix(t *testing.T) {
	assert.Equal(t, "", shortcmd.Suffix("", "", ""))
	assert.Equal(t, "[MyScheme]", shortcmd.Suffix("MyScheme", "", ""))
	assert.Equal(t, "[MyScheme / Debug]", shortcmd.Suffix("MyScheme", "", "Debug"))
	assert.Equal(t, "[MyScheme / Smoke / Debug]", shortcmd.Suffix("MyScheme", "Smoke", "Debug"))
}
