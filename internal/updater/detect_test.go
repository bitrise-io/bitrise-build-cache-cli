//go:build unit

package updater

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectInstallMethod_truthTable(t *testing.T) {
	cases := []struct {
		executable string
		want       InstallMethod
		name       string
	}{
		{"/opt/homebrew/Cellar/bitrise-build-cache-cli/2.8.4/bin/bitrise-build-cache", InstallBrew, "apple-silicon-brew"},
		{"/usr/local/Cellar/bitrise-build-cache-cli/2.8.4/bin/bitrise-build-cache", InstallBrew, "intel-brew"},
		{"/home/linuxbrew/.linuxbrew/Cellar/bitrise-build-cache-cli/2.8.4/bin/bitrise-build-cache", InstallBrew, "linuxbrew-shared"},
		{"/home/alice/.linuxbrew/Cellar/bitrise-build-cache-cli/2.8.4/bin/bitrise-build-cache", InstallBrew, "linuxbrew-user"},
		{"/opt/homebrew/bin/bitrise-build-cache", InstallBrew, "brew-symlink-prefix"},
		{"/usr/local/bin/bitrise-build-cache", InstallManual, "default-manual-prefix"},
		{"/home/alice/.local/bin/bitrise-build-cache", InstallManual, "user-local"},
		{"/tmp/bin/bitrise-build-cache", InstallManual, "ephemeral-bindir"},
		{"./bitrise-build-cache", InstallManual, "relative-path"},
		{"", InstallUnknown, "empty"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DetectInstallMethod(tc.executable))
		})
	}
}

func TestInstallMethod_String(t *testing.T) {
	assert.Equal(t, "brew", InstallBrew.String())
	assert.Equal(t, "manual", InstallManual.String())
	assert.Equal(t, "unknown", InstallUnknown.String())
}

func TestDetectInstallMethod_honoursHomebrewPrefix(t *testing.T) {
	// Path that doesn't match any hard-coded brewSubstring, but
	// $HOMEBREW_PREFIX points at its parent — classify as brew.
	t.Setenv("HOMEBREW_PREFIX", "/srv/brew-relocated")
	assert.Equal(t, InstallBrew, DetectInstallMethod("/srv/brew-relocated/bin/bitrise-build-cache"))
}

func TestDetectInstallMethod_homebrewPrefixSetButBinaryElsewhere(t *testing.T) {
	t.Setenv("HOMEBREW_PREFIX", "/srv/brew-relocated")
	// HOMEBREW_PREFIX is set but the binary lives outside it — still manual.
	assert.Equal(t, InstallManual, DetectInstallMethod("/home/alice/.local/bin/bitrise-build-cache"))
}
