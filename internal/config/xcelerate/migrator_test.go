//go:build unit

package xcelerate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

func writeStoredConfig(t *testing.T, home string, cfg map[string]any) string {
	t.Helper()

	dir := paths.FromHome(home).XcelerateRoot()
	require.NoError(t, os.MkdirAll(dir, 0o755))

	path := filepath.Join(dir, "config.json")
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path
}

func readStoredConfig(t *testing.T, path string) map[string]any {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))

	return out
}

func TestConfigMigrator_EnablesCompressionOnConfigsThatPredateIt(t *testing.T) {
	tests := []struct {
		name    string
		version any
	}{
		{"1.0.0", "1.0.0"},
		{"no version", nil},
		{"garbage version", "not-a-version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			stored := map[string]any{
				"buildCacheEnabled":  true,
				"buildCacheEndpoint": "grpcs://example.test",
				"pushEnabled":        true,
			}
			if tt.version != nil {
				stored["configVersion"] = tt.version
			}
			path := writeStoredConfig(t, home, stored)

			require.NoError(t, xcelerate.ConfigMigrator{Logger: log.NewLogger()}.Migrate(home))

			got := readStoredConfig(t, path)
			assert.Equal(t, toolconfig.XcelerateConfigVersion, got["configVersion"])
			assert.Equal(t, true, got["remoteCacheCompression"])
			assert.EqualValues(t, xcelerate.DefaultCompressionThreshold, got["remoteCacheCompressionThreshold"])
			assert.Equal(t, "grpcs://example.test", got["buildCacheEndpoint"], "unrelated settings are kept")
			assert.Equal(t, true, got["pushEnabled"])
		})
	}
}

func TestConfigMigrator_LeavesAnExplicitChoiceAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := writeStoredConfig(t, home, map[string]any{
		"configVersion":     toolconfig.XcelerateConfigVersion,
		"buildCacheEnabled": true,
	})

	require.NoError(t, xcelerate.ConfigMigrator{Logger: log.NewLogger()}.Migrate(home))

	got := readStoredConfig(t, path)
	assert.NotContains(t, got, "remoteCacheCompression", "a config at the current version that opted out stays opted out")
}

func TestConfigMigrator_MissingConfigIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, xcelerate.ConfigMigrator{Logger: log.NewLogger()}.Migrate(home))
}
