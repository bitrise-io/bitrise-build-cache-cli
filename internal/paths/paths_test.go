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

func TestPaths_bitriseRoot(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.bitrise", p.BitriseRoot())
	assert.Equal(t, "/h/.bitrise/bin", p.BitriseBinDir())
	assert.Equal(t, "/h/.bitrise/bin/bitrise-build-cache", p.BitriseBinFile("bitrise-build-cache"))
	assert.Equal(t, "/h/.bitrise/cache/ccache", p.BitriseCacheDir("ccache"))
	assert.Equal(t, "/h/.bitrise/cache/reactnative/config.json", p.BitriseCacheFile("reactnative", "config.json"))
}

func TestPaths_xcelerate(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.bitrise-xcelerate", p.XcelerateRoot())
	assert.Equal(t, "/h/.bitrise-xcelerate/config.json", p.XcelerateConfigFile())
	assert.Equal(t, "/h/.bitrise-xcelerate/bin", p.XcelerateBinDir())
	assert.Equal(t, "/h/.bitrise-xcelerate/bin/xcodebuild", p.XcelerateBinFile("xcodebuild"))
}

func TestPaths_proxySocket(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/tmp/xcelerate-proxy.sock", p.ProxySocketPath("/tmp"))
}
