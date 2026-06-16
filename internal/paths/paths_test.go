//go:build unit

package paths

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromHome_stateAndLogPaths(t *testing.T) {
	p := FromHome("/Users/alice")

	assert.Equal(t, "/Users/alice/.local/state/bitrise-build-cache", p.StateDir())
	assert.Equal(t, "/Users/alice/.local/state/bitrise-build-cache/version-state.json", p.StateFile("version-state.json"))
	assert.Equal(t, "/Users/alice/.local/state/bitrise-build-cache/logs", p.DaemonLogDir())
	assert.Equal(t, "/Users/alice/Library/LaunchAgents", p.LaunchAgentsDir())
	assert.Equal(t, "/Users/alice/.config/systemd/user", p.SystemdUserDir())
}

func TestPaths_supervisorArtifacts(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, filepath.Join("/h", "Library/LaunchAgents", "io.bitrise.build-cache.xcelerate-proxy.plist"),
		p.PlistPath("io.bitrise.build-cache.xcelerate-proxy"))
	assert.Equal(t, filepath.Join("/h", ".config/systemd/user", "bitrise-build-cache-xcelerate-proxy.service"),
		p.UnitPath("bitrise-build-cache-xcelerate-proxy"))
	assert.Equal(t, filepath.Join("/h", ".local/state/bitrise-build-cache/logs", "ccache-helper.out.log"),
		p.DaemonStdoutPath("ccache-helper"))
	assert.Equal(t, filepath.Join("/h", ".local/state/bitrise-build-cache/logs", "ccache-helper.err.log"),
		p.DaemonStderrPath("ccache-helper"))
}
