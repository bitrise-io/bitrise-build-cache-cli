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

	entries, err := os.ReadDir(p.BuildCacheMachineConfigDir())
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "temp file should be renamed away")
	}
}

func TestRead_MalformedJSONSurfacesError(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.BuildCacheMachineConfigDir(), 0o755))
	require.NoError(t, os.WriteFile(p.MachineConfigFile(), []byte(`{not json`), 0o644))

	_, err := Read(utils.DefaultOsProxy{}, p, nil)
	require.Error(t, err)
}

func TestRead_UnknownModeNormalisesToAlways(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.BuildCacheMachineConfigDir(), 0o755))
	require.NoError(t, os.WriteFile(p.MachineConfigFile(), []byte(`{"project_mode":"weird"}`), 0o644))

	cfg, err := Read(utils.DefaultOsProxy{}, p, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeAlways, cfg.ProjectMode)
}

func TestEffective(t *testing.T) {
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
			got, err := Effective(tc.flag, tc.current)
			if tc.wantErr {
				require.Error(t, err)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValidateFlag(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateFlag(""))
	require.NoError(t, ValidateFlag(string(ModeAlways)))
	require.NoError(t, ValidateFlag(string(ModeOptIn)))
	require.Error(t, ValidateFlag("garbage"))
}

func TestWrite_CreatesParentDir(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)

	// The parent dir does not exist yet.
	_, err := os.Stat(p.BuildCacheMachineConfigDir())
	require.True(t, os.IsNotExist(err))

	require.NoError(t, Write(Config{ProjectMode: ModeAlways}, utils.DefaultOsProxy{}, p))

	info, err := os.Stat(p.MachineConfigFile())
	require.NoError(t, err)
	assert.False(t, info.IsDir())
	assert.Equal(t, filepath.Base(p.MachineConfigFile()), info.Name())
}
