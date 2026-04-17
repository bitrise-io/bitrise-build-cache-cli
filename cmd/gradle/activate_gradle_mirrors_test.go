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

func TestActivateGradleMirrorsFn(t *testing.T) {
	initFileName := "bitrise-gradle-mirrors.init.gradle.kts"
	allMirrors := gradle.KnownMirrors
	centralOnly := []gradle.RepoMirror{gradle.KnownMirrors[0]}
	googleOnly := []gradle.RepoMirror{gradle.KnownMirrors[1]}

	tests := []struct {
		name             string
		envs             map[string]string
		mirrors          []gradle.RepoMirror
		expectCreated    bool
		expectContains   []string
		expectNotContain []string
	}{
		{
			name:          "disabled when env not set",
			envs:          map[string]string{},
			mirrors:       allMirrors,
			expectCreated: false,
		},
		{
			name:          "disabled when env is false",
			envs:          map[string]string{"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "false"},
			mirrors:       allMirrors,
			expectCreated: false,
		},
		{
			name:          "disabled when enabled but no datacenter",
			envs:          map[string]string{"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "true"},
			mirrors:       allMirrors,
			expectCreated: false,
		},
		{
			name: "AMS1 all mirrors",
			envs: map[string]string{
				"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "true",
				"BITRISE_DEN_VM_DATACENTER":          "AMS1",
			},
			mirrors:       allMirrors,
			expectCreated: true,
			expectContains: []string{
				"https://repository-manager-ams.services.bitrise.io:8090/maven/central",
				"https://repository-manager-ams.services.bitrise.io:8090/maven/google",
			},
		},
		{
			name: "IAD1 mavencentral only",
			envs: map[string]string{
				"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "true",
				"BITRISE_DEN_VM_DATACENTER":          "IAD1",
			},
			mirrors:       centralOnly,
			expectCreated: true,
			expectContains: []string{
				"https://repository-manager-iad.services.bitrise.io:8090/maven/central",
			},
			expectNotContain: []string{
				"https://repository-manager-iad.services.bitrise.io:8090/maven/google",
			},
		},
		{
			name: "ORD1 google only",
			envs: map[string]string{
				"BITRISE_MAVENCENTRAL_PROXY_ENABLED": "true",
				"BITRISE_DEN_VM_DATACENTER":          "ORD1",
			},
			mirrors:       googleOnly,
			expectCreated: true,
			expectContains: []string{
				"https://repository-manager-ord.services.bitrise.io:8090/maven/google",
			},
			expectNotContain: []string{
				"https://repository-manager-ord.services.bitrise.io:8090/maven/central",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			err := gradle.ActivateGradleMirrorsFn(mockLogger, tmpDir, tt.envs, tt.mirrors)
			require.NoError(t, err)

			initFile := filepath.Join(tmpDir, "init.d", initFileName)

			if !tt.expectCreated {
				_, statErr := os.Stat(initFile)
				assert.True(t, os.IsNotExist(statErr), "init script should not be created")

				return
			}

			content, readErr := os.ReadFile(initFile)
			require.NoError(t, readErr, "init script should be created")

			for _, s := range tt.expectContains {
				assert.Contains(t, string(content), s)
			}

			for _, s := range tt.expectNotContain {
				assert.NotContains(t, string(content), s)
			}

			assert.NotContains(t, string(content), "{{ .MirrorURL }}")
			assert.NotContains(t, string(content), "{{ .GradleMatch }}")
		})
	}
}
