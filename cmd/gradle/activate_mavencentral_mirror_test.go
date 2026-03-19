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

	initFile := filepath.Join(tmpDir, "init.d", "bitrise-mavencentral-mirror.init.gradle.kts")
	_, statErr := os.Stat(initFile)
	assert.True(t, os.IsNotExist(statErr), "init script should not be created when env var is not set")
}

func TestActivateMavenCentralMirrorFn_EnabledFalse(t *testing.T) {
	tmpDir := t.TempDir()

	err := gradle.ActivateMavenCentralMirrorFn(mockLogger, tmpDir, func(key string) string {
		if key == "BITRISE_MAVENCENTRAL_PROXY_ENABLED" {
			return "false"
		}

		return ""
	})

	require.NoError(t, err)

	initFile := filepath.Join(tmpDir, "init.d", "bitrise-mavencentral-mirror.init.gradle.kts")
	_, statErr := os.Stat(initFile)
	assert.True(t, os.IsNotExist(statErr), "init script should not be created when env var is 'false'")
}

func TestActivateMavenCentralMirrorFn_EnabledNoDatacenter(t *testing.T) {
	tmpDir := t.TempDir()

	err := gradle.ActivateMavenCentralMirrorFn(mockLogger, tmpDir, func(key string) string {
		if key == "BITRISE_MAVENCENTRAL_PROXY_ENABLED" {
			return "true"
		}

		return ""
	})

	require.NoError(t, err)

	initFile := filepath.Join(tmpDir, "init.d", "bitrise-mavencentral-mirror.init.gradle.kts")
	_, statErr := os.Stat(initFile)
	assert.True(t, os.IsNotExist(statErr), "init script should not be created when datacenter is not set")
}

func TestActivateMavenCentralMirrorFn_EnabledAMS1(t *testing.T) {
	tmpDir := t.TempDir()

	err := gradle.ActivateMavenCentralMirrorFn(mockLogger, tmpDir, func(key string) string {
		switch key {
		case "BITRISE_MAVENCENTRAL_PROXY_ENABLED":
			return "true"
		case "BITRISE_DEN_VM_DATACENTER":
			return "AMS1"
		}

		return ""
	})

	require.NoError(t, err)

	initFile := filepath.Join(tmpDir, "init.d", "bitrise-mavencentral-mirror.init.gradle.kts")
	content, readErr := os.ReadFile(initFile)
	require.NoError(t, readErr, "init script should be created")
	assert.Contains(t, string(content), "https://repository-manager-ams.services.bitrise.io:8090/maven/central")
	assert.NotContains(t, string(content), "{{MIRROR_URL}}")
}

func TestActivateMavenCentralMirrorFn_EnabledIAD1(t *testing.T) {
	tmpDir := t.TempDir()

	err := gradle.ActivateMavenCentralMirrorFn(mockLogger, tmpDir, func(key string) string {
		switch key {
		case "BITRISE_MAVENCENTRAL_PROXY_ENABLED":
			return "true"
		case "BITRISE_DEN_VM_DATACENTER":
			return "IAD1"
		}

		return ""
	})

	require.NoError(t, err)

	initFile := filepath.Join(tmpDir, "init.d", "bitrise-mavencentral-mirror.init.gradle.kts")
	content, readErr := os.ReadFile(initFile)
	require.NoError(t, readErr, "init script should be created")
	assert.Contains(t, string(content), "https://repository-manager-iad.services.bitrise.io:8090/maven/central")
}

func TestActivateMavenCentralMirrorFn_EnabledORD1(t *testing.T) {
	tmpDir := t.TempDir()

	err := gradle.ActivateMavenCentralMirrorFn(mockLogger, tmpDir, func(key string) string {
		switch key {
		case "BITRISE_MAVENCENTRAL_PROXY_ENABLED":
			return "true"
		case "BITRISE_DEN_VM_DATACENTER":
			return "ORD1"
		}

		return ""
	})

	require.NoError(t, err)

	initFile := filepath.Join(tmpDir, "init.d", "bitrise-mavencentral-mirror.init.gradle.kts")
	content, readErr := os.ReadFile(initFile)
	require.NoError(t, readErr, "init script should be created")
	assert.Contains(t, string(content), "https://repository-manager-ord.services.bitrise.io:8090/maven/central")
}
