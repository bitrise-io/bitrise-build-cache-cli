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

func TestEffectiveProjectMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flag    string
		current Mode
		want    Mode
		wantErr bool
	}{
		{name: "flag wins over current", flag: "opt-in", current: ModeAlways, want: ModeOptIn},
		{name: "empty flag uses current", flag: "", current: ModeOptIn, want: ModeOptIn},
		{name: "empty flag + empty current falls back to always", flag: "", current: "", want: ModeAlways},
		{name: "invalid flag returns error", flag: "garbage", current: ModeOptIn, wantErr: true},
		{name: "invalid flag + empty current returns error", flag: "garbage", current: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EffectiveProjectMode(tc.flag, tc.current)
			if tc.wantErr {
				require.Error(t, err)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValidateProjectModeFlag(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateProjectModeFlag(""))
	require.NoError(t, ValidateProjectModeFlag(string(ModeAlways)))
	require.NoError(t, ValidateProjectModeFlag(string(ModeOptIn)))
	require.Error(t, ValidateProjectModeFlag("garbage"))
}

func TestEffectiveCachePush(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		name        string
		flagChanged bool
		flagValue   bool
		current     *bool
		want        bool
	}{
		{name: "flag changed true wins over stored false", flagChanged: true, flagValue: true, current: &falseVal, want: true},
		{name: "flag changed false wins over stored true", flagChanged: true, flagValue: false, current: &trueVal, want: false},
		{name: "no flag uses stored true", flagChanged: false, flagValue: false, current: &trueVal, want: true},
		{name: "no flag + no stored falls back to default true", flagChanged: false, flagValue: false, current: nil, want: DefaultCachePush},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, EffectiveCachePush(tc.flagChanged, tc.flagValue, tc.current))
		})
	}
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

func TestStoredCachePush_MissingConfigReturnsDefault(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	got, err := StoredCachePush(utils.DefaultOsProxy{}, paths.FromHome(home), nil)
	require.NoError(t, err)
	assert.Equal(t, DefaultCachePush, got)
}

func TestStoredCachePush_ReadsPersistedValue(t *testing.T) {
	t.Parallel()

	falseVal := false
	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, Write(Config{CachePush: &falseVal}, utils.DefaultOsProxy{}, p))

	got, err := StoredCachePush(utils.DefaultOsProxy{}, p, nil)
	require.NoError(t, err)
	assert.False(t, got)
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
