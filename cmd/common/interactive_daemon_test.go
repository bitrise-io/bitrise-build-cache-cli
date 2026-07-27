//go:build unit

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
)

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
	assert.Equal(t, []string{daemonpkg.ServiceXcelerateProxy}, names("xcode"))
	assert.Equal(t, []string{daemonpkg.ServiceCcacheHelper}, names("ccache"))
	assert.Equal(t,
		[]string{daemonpkg.ServiceXcelerateProxy, daemonpkg.ServiceCcacheHelper},
		names("gradle", "xcode", "ccache"))
}
