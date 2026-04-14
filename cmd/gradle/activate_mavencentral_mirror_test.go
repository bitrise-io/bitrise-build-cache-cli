//go:build unit

package gradle_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/gradle"
)

func TestActivateMavenCentralMirrorFn(t *testing.T) {
	initFileName := "bitrise-mavencentral-mirror.init.gradle.kts"

	tests := []struct {
		name           string
		envs           map[string]string
		expectCreated  bool
		expectContains string
	}{
		{
			name:          "disabled when env not set",
			envs:          map[string]string{},
			expectCreated: false,
		},
		{
			name:          "disabled when env is false",
			envs:          map[string]string{"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "false"},
			expectCreated: false,
		},
		{
			name:          "disabled when enabled but no datacenter",
			envs:          map[string]string{"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "true"},
			expectCreated: false,
		},
		{
			name: "AMS1 datacenter",
			envs: map[string]string{
				"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "true",
				"BITRISE_DEN_VM_DATACENTER":          "AMS1",
			},
			expectCreated:  true,
			expectContains: "https://repository-manager-ams.services.bitrise.io:8090/maven/central",
		},
		{
			name: "IAD1 datacenter",
			envs: map[string]string{
				"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "true",
				"BITRISE_DEN_VM_DATACENTER":          "IAD1",
			},
			expectCreated:  true,
			expectContains: "https://repository-manager-iad.services.bitrise.io:8090/maven/central",
		},
		{
			name: "ORD1 datacenter",
			envs: map[string]string{
				"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "true",
				"BITRISE_DEN_VM_DATACENTER":          "ORD1",
			},
			expectCreated:  true,
			expectContains: "https://repository-manager-ord.services.bitrise.io:8090/maven/central",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			err := gradle.ActivateMavenCentralMirrorFn(mockLogger, tmpDir, tt.envs)
			require.NoError(t, err)

			initFile := filepath.Join(tmpDir, "init.d", initFileName)

			if !tt.expectCreated {
				_, statErr := os.Stat(initFile)
				assert.True(t, os.IsNotExist(statErr), "init script should not be created")

				return
			}

			content, readErr := os.ReadFile(initFile)
			require.NoError(t, readErr, "init script should be created")
			assert.Contains(t, string(content), tt.expectContains)
			assert.NotContains(t, string(content), "{{ .MirrorURL }}")
		})
	}
}
