//go:build unit

package gradle_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/gradle"
)

func TestActivateMavenCentralMirrorFn_Disabled(t *testing.T) {
	tmpDir := t.TempDir()

	err := gradle.ActivateMavenCentralMirrorFn(mockLogger, tmpDir, func(key string) string {
		return ""
	})

	require.NoError(t, err)

	// Init script should NOT be created
	initFile := filepath.Join(tmpDir, "init.d", "bitrise-mavencentral-mirror.init.gradle.kts")
	_, statErr := os.Stat(initFile)
	assert.True(t, os.IsNotExist(statErr), "init script should not be created when env var is not set")
}

func TestActivateMavenCentralMirrorFn_EnabledFalse(t *testing.T) {
	tmpDir := t.TempDir()

	err := gradle.ActivateMavenCentralMirrorFn(mockLogger, tmpDir, func(key string) string {
		return "false"
	})

	require.NoError(t, err)

	initFile := filepath.Join(tmpDir, "init.d", "bitrise-mavencentral-mirror.init.gradle.kts")
	_, statErr := os.Stat(initFile)
	assert.True(t, os.IsNotExist(statErr), "init script should not be created when env var is 'false'")
}

func TestActivateMavenCentralMirrorFn_Enabled(t *testing.T) {
	tmpDir := t.TempDir()

	err := gradle.ActivateMavenCentralMirrorFn(mockLogger, tmpDir, func(key string) string {
		if key == "BITRISE_MAVENCENTRAL_PROXY_ENABLED" {
			return "true"
		}

		return ""
	})

	require.NoError(t, err)

	initFile := filepath.Join(tmpDir, "init.d", "bitrise-mavencentral-mirror.init.gradle.kts")
	content, readErr := os.ReadFile(initFile)
	require.NoError(t, readErr, "init script should be created when env var is 'true'")
	assert.Contains(t, string(content), "repository-manager.services.bitrise.dev/maven/central")
	assert.Contains(t, string(content), "InternalRepositoryPlugin")
}
