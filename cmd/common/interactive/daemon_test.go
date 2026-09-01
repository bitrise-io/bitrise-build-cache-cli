//go:build unit

package interactive

import (
	"bytes"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// huh's accessible mode walks every group unconditionally, ignoring hide funcs —
// so a conditional question has to be built conditionally, not hidden. If this
// ever starts passing, the wizard can go back to one form with a hide func.
func TestHuhAccessibleModeIgnoresGroupHideFunc(t *testing.T) {
	var answer bool

	group := huh.NewGroup(
		huh.NewConfirm().Title("should be hidden").Value(&answer),
	).WithHideFunc(func() bool { return true })

	var out bytes.Buffer
	require.NoError(t, huh.NewForm(group).
		WithAccessible(true).
		WithOutput(&out).
		WithInput(strings.NewReader("n\n")).
		Run())

	assert.Contains(t, out.String(), "should be hidden",
		"huh accessible mode prompts hidden groups; the wizard must not rely on WithHideFunc")
}

func TestDaemonServicesForTools(t *testing.T) {
	names := func(tools ...string) []string {
		svcs := daemonServicesForTools(tools)
		out := make([]string, 0, len(svcs))
		for _, s := range svcs {
			out = append(out, s.Name)
		}

		return out
	}

	assert.Empty(t, names(), "no tools, no services")
	assert.Empty(t, names("gradle", "bazel"), "Gradle and Bazel talk to the cache directly")
	// No tool needs a supervised service: each is started on demand by the
	// process that needs it, which keeps it in the build's resource coalition.
	// See docs/daemon-latency.md.
	assert.Empty(t, names("xcode"), "the xcelerate proxy is never supervised")
	assert.Empty(t, names("ccache"), "the ccache helper is never supervised")
	assert.Empty(t, names("gradle", "xcode", "ccache"))
}
