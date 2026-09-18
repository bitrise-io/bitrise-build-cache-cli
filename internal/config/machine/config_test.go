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

func TestEffective_ProjectMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		flag       string
		current    Mode
		wantMode   Mode
		wantSource string
		wantErr    bool
	}{
		{name: "flag wins over current", flag: "opt-in", current: ModeAlways, wantMode: ModeOptIn, wantSource: SourceFlag},
		{name: "empty flag uses current", flag: "", current: ModeOptIn, wantMode: ModeOptIn, wantSource: SourceMachineConfig},
		{name: "empty flag + empty current falls back to always", flag: "", current: "", wantMode: ModeAlways, wantSource: SourceDefault},
		{name: "invalid flag returns error", flag: "garbage", current: ModeOptIn, wantErr: true},
		{name: "invalid flag + empty current returns error", flag: "garbage", current: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, sources, err := Effective(FlagOverlay{ProjectMode: tc.flag}, Config{ProjectMode: tc.current})
			if tc.wantErr {
				require.Error(t, err)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantMode, got.ProjectMode)
			assert.Equal(t, tc.wantSource, sources.ProjectMode)
		})
	}
}

func TestEffective_CachePush(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		name       string
		flag       *bool
		current    *bool
		want       bool
		wantSource string
	}{
		{name: "flag true wins over stored false", flag: &trueVal, current: &falseVal, want: true, wantSource: SourceFlag},
		{name: "flag false wins over stored true", flag: &falseVal, current: &trueVal, want: false, wantSource: SourceFlag},
		{name: "no flag uses stored true", flag: nil, current: &trueVal, want: true, wantSource: SourceMachineConfig},
		{name: "no flag + no stored falls back to default true", flag: nil, current: nil, want: DefaultCachePush, wantSource: SourceDefault},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, sources, err := Effective(FlagOverlay{CachePush: tc.flag}, Config{CachePush: tc.current})
			require.NoError(t, err)
			require.NotNil(t, got.CachePush)
			assert.Equal(t, tc.want, *got.CachePush)
			assert.Equal(t, tc.wantSource, sources.CachePush)
		})
	}
}

func TestEffective_AllEmptyResolvesToDefaults(t *testing.T) {
	t.Parallel()

	got, sources, err := Effective(FlagOverlay{}, Config{})
	require.NoError(t, err)

	assert.Equal(t, ModeAlways, got.ProjectMode)
	require.NotNil(t, got.CachePush)
	assert.Equal(t, DefaultCachePush, *got.CachePush)

	assert.Equal(t, SourceDefault, sources.ProjectMode)
	assert.Equal(t, SourceDefault, sources.CachePush)
}

func TestEffective_OverlayWinsOverStored(t *testing.T) {
	t.Parallel()

	flagPush := false
	storedPush := true

	got, sources, err := Effective(
		FlagOverlay{ProjectMode: string(ModeOptIn), CachePush: &flagPush},
		Config{ProjectMode: ModeAlways, CachePush: &storedPush},
	)
	require.NoError(t, err)

	assert.Equal(t, ModeOptIn, got.ProjectMode)
	require.NotNil(t, got.CachePush)
	assert.False(t, *got.CachePush)

	assert.Equal(t, SourceFlag, sources.ProjectMode)
	assert.Equal(t, SourceFlag, sources.CachePush)
}

func TestEffective_StoredWinsOverDefault(t *testing.T) {
	t.Parallel()

	storedPush := false

	got, sources, err := Effective(
		FlagOverlay{},
		Config{ProjectMode: ModeOptIn, CachePush: &storedPush},
	)
	require.NoError(t, err)

	assert.Equal(t, ModeOptIn, got.ProjectMode)
	require.NotNil(t, got.CachePush)
	assert.False(t, *got.CachePush)

	assert.Equal(t, SourceMachineConfig, sources.ProjectMode)
	assert.Equal(t, SourceMachineConfig, sources.CachePush)
}

func TestEffective_InvalidProjectModeReturnsError(t *testing.T) {
	t.Parallel()

	_, _, err := Effective(FlagOverlay{ProjectMode: "garbage"}, Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "garbage")
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
