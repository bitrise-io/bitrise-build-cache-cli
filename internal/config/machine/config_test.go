//go:build unit

package machine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestRead_MissingFileReturnsEmptyConfig(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cfg, err := Read(utils.DefaultOsProxy{}, paths.FromHome(home), nil)

	require.NoError(t, err)
	assert.Equal(t, Config{}, cfg)
}

func TestWriteThenRead_Roundtrip(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)

	require.NoError(t, Write(Config{ProjectMode: ModeOptIn}, utils.DefaultOsProxy{}, p))

	cfg, err := Read(utils.DefaultOsProxy{}, p, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeOptIn, cfg.ProjectMode)

	body, err := os.ReadFile(p.MachineConfigFile())
	require.NoError(t, err)
	assert.Contains(t, string(body), `"project_mode": "opt-in"`)
}

func TestWrite_AtomicRenameLeavesNoTempFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, Write(Config{ProjectMode: ModeAlways}, utils.DefaultOsProxy{}, p))

	entries, err := os.ReadDir(p.BitriseCacheRoot())
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "temp file should be renamed away")
	}
}

func TestRead_MalformedJSONSurfacesError(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.BitriseCacheRoot(), 0o755))
	require.NoError(t, os.WriteFile(p.MachineConfigFile(), []byte(`{not json`), 0o644))

	_, err := Read(utils.DefaultOsProxy{}, p, nil)
	require.Error(t, err)
}

func TestRead_UnknownModeNormalisesToAlways(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.BitriseCacheRoot(), 0o755))
	require.NoError(t, os.WriteFile(p.MachineConfigFile(), []byte(`{"project_mode":"weird"}`), 0o644))

	cfg, err := Read(utils.DefaultOsProxy{}, p, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeAlways, cfg.ProjectMode)
}

func TestResolvedProjectMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		want Mode
	}{
		{name: "empty falls back to always", cfg: Config{}, want: ModeAlways},
		{name: "always stays always", cfg: Config{ProjectMode: ModeAlways}, want: ModeAlways},
		{name: "opt-in stays opt-in", cfg: Config{ProjectMode: ModeOptIn}, want: ModeOptIn},
		{name: "unknown falls back to always", cfg: Config{ProjectMode: "garbage"}, want: ModeAlways},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ResolvedProjectMode(tc.cfg))
		})
	}
}

func TestResolvedCachePush(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{name: "nil falls back to default", cfg: Config{}, want: DefaultCachePush},
		{name: "stored true", cfg: Config{CachePush: &trueVal}, want: true},
		{name: "stored false", cfg: Config{CachePush: &falseVal}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ResolvedCachePush(tc.cfg))
		})
	}
}

func TestValidateProjectMode(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateProjectMode(string(ModeAlways)))
	require.NoError(t, ValidateProjectMode(string(ModeOptIn)))

	err := ValidateProjectMode("garbage")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "garbage")

	require.Error(t, ValidateProjectMode(""))
}

func TestCachePush_JSONRoundtrip(t *testing.T) {
	t.Parallel()

	trueVal := true
	home := t.TempDir()
	p := paths.FromHome(home)

	require.NoError(t, Write(Config{CachePush: &trueVal}, utils.DefaultOsProxy{}, p))

	cfg, err := Read(utils.DefaultOsProxy{}, p, nil)
	require.NoError(t, err)
	require.NotNil(t, cfg.CachePush)
	assert.True(t, *cfg.CachePush)

	body, err := os.ReadFile(p.MachineConfigFile())
	require.NoError(t, err)
	assert.Contains(t, string(body), `"cache_push": true`)
}

func TestCachePush_JSONRoundtripFalse(t *testing.T) {
	t.Parallel()

	falseVal := false
	home := t.TempDir()
	p := paths.FromHome(home)

	require.NoError(t, Write(Config{CachePush: &falseVal}, utils.DefaultOsProxy{}, p))

	cfg, err := Read(utils.DefaultOsProxy{}, p, nil)
	require.NoError(t, err)
	require.NotNil(t, cfg.CachePush)
	assert.False(t, *cfg.CachePush)

	body, err := os.ReadFile(p.MachineConfigFile())
	require.NoError(t, err)
	assert.Contains(t, string(body), `"cache_push": false`)
}

func TestCachePush_OmittedWhenNil(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)

	require.NoError(t, Write(Config{ProjectMode: ModeAlways}, utils.DefaultOsProxy{}, p))

	body, err := os.ReadFile(p.MachineConfigFile())
	require.NoError(t, err)
	assert.NotContains(t, string(body), "cache_push", "nil CachePush should be omitted from JSON")
}

func TestWrite_CreatesParentDir(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)

	_, err := os.Stat(p.BitriseCacheRoot())
	require.True(t, os.IsNotExist(err))

	require.NoError(t, Write(Config{ProjectMode: ModeAlways}, utils.DefaultOsProxy{}, p))

	info, err := os.Stat(p.MachineConfigFile())
	require.NoError(t, err)
	assert.False(t, info.IsDir())
	assert.Equal(t, filepath.Base(p.MachineConfigFile()), info.Name())
}
