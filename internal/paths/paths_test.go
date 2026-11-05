//go:build unit

package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromHome_stateAndLogPaths(t *testing.T) {
	p := FromHome("/Users/alice")

	assert.Equal(t, "/Users/alice/.local/state/bitrise-build-cache", p.StateDir())
	assert.Equal(t, "/Users/alice/.local/state/bitrise-build-cache/version-state.json", p.StateFile("version-state.json"))
	assert.Equal(t, "/Users/alice/Library/LaunchAgents", p.LaunchAgentsDir())
	assert.Equal(t, "/Users/alice/.config/systemd/user", p.SystemdUserDir())
}

func TestPaths_supervisorArtifacts(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, filepath.Join("/h", "Library/LaunchAgents", "io.bitrise.build-cache.xcelerate-proxy.plist"),
		p.PlistPath("io.bitrise.build-cache.xcelerate-proxy"))
	assert.Equal(t, filepath.Join("/h", ".config/systemd/user", "bitrise-build-cache-xcelerate-proxy.service"),
		p.UnitPath("bitrise-build-cache-xcelerate-proxy"))
}

func TestPaths_bitriseRoot(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.bitrise", p.BitriseRoot())
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

func TestPaths_xcodeApp(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.bitrise-xcelerate/xcode-app.xcconfig", p.XcodeAppOverrideXCConfigFile())
	assert.Equal(t, ".bitrise-build-cache.xcconfig", XcodeAppSiblingXCConfigFileName)
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
	assert.Equal(t, "/h/.bitrise/cache/xcode-dd", p.XcodeManagedDerivedDataRoot())
	assert.Equal(t, "/h/.bitrise/cache/xcode-cas", p.XcodeManagedCASDir())
}

func TestPaths_xcelerateState(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.local/state/xcelerate", p.XcelerateStateDir())
	assert.Equal(t, "/h/.local/state/xcelerate/logs", p.XcelerateLogDir())
	assert.Equal(t, "/h/.local/state/xcelerate/sessions", p.XcelerateSessionsDir())
}

func TestPaths_xcelerateEnrichment(t *testing.T) {
	p := FromHome("/h")

	assert.Equal(t, "/h/.local/state/xcelerate/enrichment", p.XcelerateEnrichmentDir())
	assert.Equal(t, "/h/.local/state/xcelerate/enrichment/handled-manifests.ndjson", p.HandledManifestsFile())
	assert.Equal(t, "/h/.local/state/xcelerate/enrichment/pending-invocations.ndjson", p.PendingInvocationsFile())
	assert.Equal(t, "/h/.local/state/xcelerate/enrichment/health.json", p.EnrichmentHealthFile())
}

func TestRepoLocalConfigPath(t *testing.T) {
	assert.Equal(t, "/repo/.bitrise-build-cache/xcode-build.json", RepoLocalConfigPath("/repo", "xcode-build.json"))
}

func TestXcodeSelectLinkCASPluginPath(t *testing.T) {
	assert.Equal(t, "/var/db/xcode_select_link/usr/lib/libToolchainCASPlugin.dylib", XcodeSelectLinkCASPluginPath())
}

func TestDerivedDataFromManifestPath_ValidShape(t *testing.T) {
	// Canonical IDE layout: <DD>/Logs/Build/LogStoreManifest.plist
	assert.Equal(t,
		"/Users/alice/Library/Developer/Xcode/DerivedData/App-abc123",
		DerivedDataFromManifestPath("/Users/alice/Library/Developer/Xcode/DerivedData/App-abc123/Logs/Build/LogStoreManifest.plist"),
	)

	// Different sub-directory under Logs (Test action) still resolves to DD root.
	assert.Equal(t,
		"/project/DD",
		DerivedDataFromManifestPath("/project/DD/Logs/Test/LogStoreManifest.plist"),
	)

	// Wrapper-managed DD layout.
	assert.Equal(t,
		"/h/.bitrise/cache/xcode-dd/sha",
		DerivedDataFromManifestPath("/h/.bitrise/cache/xcode-dd/sha/Logs/Build/LogStoreManifest.plist"),
	)
}

func TestDerivedDataFromManifestPath_MalformedReturnsEmpty(t *testing.T) {
	// Empty input.
	assert.Equal(t, "", DerivedDataFromManifestPath(""))

	// No "Logs" ancestor two levels up.
	assert.Equal(t, "", DerivedDataFromManifestPath("/some/random/path/LogStoreManifest.plist"))

	// "Logs" is at the wrong depth — only one level between manifest and base.
	assert.Equal(t, "", DerivedDataFromManifestPath("/Logs/LogStoreManifest.plist"))
}

type osDirMaker struct{}

func (osDirMaker) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }

func TestEnsureDir_CreatesMissingAndAcceptsExisting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "logs")

	require.NoError(t, EnsureDir(osDirMaker{}, dir))
	assert.DirExists(t, dir)

	require.NoError(t, EnsureDir(osDirMaker{}, dir), "an existing dir is not an error")
}
