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

func TestPaths_invocations(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.local/state/bitrise-build-cache/invocations", p.InvocationsDir())
	assert.Equal(t, "/h/.local/state/bitrise-build-cache/invocations/2026-06-25.ndjson",
		p.InvocationsFile("2026-06-25"))
}

func TestPaths_xcodeManagedDirs(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.bitrise/cache/xcode-dd/abc123", p.XcodeManagedDerivedDataDir("abc123"))
	assert.Equal(t, "/h/.bitrise/cache/xcode-ptd/abc123", p.XcodeManagedProjectTempDir("abc123"))
}

func TestPaths_xcelerateHandledInvocations(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.local/state/xcelerate", p.XcelerateStateDir())
	assert.Equal(t, "/h/.local/state/xcelerate/logs", p.XcelerateLogDir())
	assert.Equal(t, "/h/.local/state/xcelerate/enrichment/handled-invocations", p.XcelerateHandledInvocationDir())
	assert.Equal(t, "/h/.local/state/xcelerate/enrichment/handled-invocations/abc-123", p.XcelerateHandledInvocationFile("abc-123"))
}

func TestPaths_xcelerateEnrichment(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.local/state/xcelerate/enrichment", p.XcelerateEnrichmentDir())
	assert.Equal(t, "/h/.local/state/xcelerate/enrichment/handled-manifests.ndjson", p.HandledManifestsFile())
	assert.Equal(t, "/h/.local/state/xcelerate/enrichment/pending-invocations.ndjson", p.PendingInvocationsFile())
	assert.Equal(t, "/h/.local/state/xcelerate/enrichment/health.json", p.EnrichmentHealthFile())
}

func TestPaths_linkedProjects(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.bitrise-xcelerate/linked-projects", p.LinkedProjectsDir())

	// Same absolute path → identical state file; different paths → different files.
	one := p.LinkedProjectStateFile("/Users/a/App.xcodeproj")
	two := p.LinkedProjectStateFile("/Users/a/App.xcodeproj")
	three := p.LinkedProjectStateFile("/Users/b/App.xcodeproj")
	assert.Equal(t, one, two)
	assert.NotEqual(t, one, three)
	assert.True(t, filepath.Dir(one) == p.LinkedProjectsDir())
	assert.Regexp(t, `^/h/\.bitrise-xcelerate/linked-projects/[0-9a-f]{16}\.state\.json$`, one)
}
