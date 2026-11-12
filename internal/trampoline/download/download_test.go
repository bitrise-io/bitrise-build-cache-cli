//go:build unit

package download

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func TestEnsureForVersion_InstallsEmbeddedBytes(t *testing.T) {
	saved := embeddedTrampoline
	embeddedTrampoline = []byte("MACHO-trampoline-stub\x00")
	t.Cleanup(func() { embeddedTrampoline = saved })

	home := t.TempDir()
	p := paths.FromHome(home)

	d := Deps{Env: func(string) string { return "" }, MkdirAll: os.MkdirAll, Stat: os.Stat}

	got, err := EnsureForVersion(context.Background(), "9.9.9", p)
	require.NoError(t, err)
	assert.Equal(t, p.TrampolineBinary("9.9.9"), got)

	body, err := os.ReadFile(got)
	require.NoError(t, err)
	assert.Equal(t, embeddedTrampoline, body)

	info1, err := os.Stat(got)
	require.NoError(t, err)

	// Stamp an older mtime so the sha-match no-op path is the only way the
	// second call can leave mtime unchanged; a rewrite would move it forward.
	older := info1.ModTime().Add(-time.Hour)
	require.NoError(t, os.Chtimes(got, older, older))

	got2, err := ensure("9.9.9", p, d)
	require.NoError(t, err)
	assert.Equal(t, got, got2)

	info2, err := os.Stat(got2)
	require.NoError(t, err)
	assert.Equal(t, older, info2.ModTime(), "sha-match path must not rewrite the cached file")
}

func TestEnsureForVersion_EmptyEmbedReturnsError(t *testing.T) {
	saved := embeddedTrampoline
	embeddedTrampoline = nil
	t.Cleanup(func() { embeddedTrampoline = saved })

	p := paths.FromHome(t.TempDir())
	d := Deps{Env: func(string) string { return "" }, MkdirAll: os.MkdirAll, Stat: os.Stat}

	_, err := ensure("9.9.9", p, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scripts/build_trampoline_embed.sh")
}

func TestEnsureForVersion_EnvOverrideWinsOverEmbed(t *testing.T) {
	saved := embeddedTrampoline
	embeddedTrampoline = []byte("MACHO-trampoline-stub\x00")
	t.Cleanup(func() { embeddedTrampoline = saved })

	home := t.TempDir()
	p := paths.FromHome(home)

	override := filepath.Join(home, "local-trampoline")
	require.NoError(t, os.WriteFile(override, []byte("local-build"), 0o755))

	d := Deps{
		Env: func(k string) string {
			if k == EnvOverride {
				return override
			}

			return ""
		},
		MkdirAll: os.MkdirAll,
		Stat:     os.Stat,
	}

	got, err := ensure("9.9.9", p, d)
	require.NoError(t, err)
	assert.Equal(t, override, got, "override path must win over the embed cache path")

	_, err = os.Stat(p.TrampolineBinary("9.9.9"))
	assert.True(t, os.IsNotExist(err), "embed must not write the cache when override is set")
}

func TestEnsureForVersion_EnvOverrideMissingFails(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	d := Deps{
		Env: func(k string) string {
			if k == EnvOverride {
				return "/does/not/exist/trampoline"
			}

			return ""
		},
		Stat: os.Stat,
	}

	_, err := ensure("9.9.9", p, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "override")
}

func TestEnsureForVersion_StripsLeadingV(t *testing.T) {
	saved := embeddedTrampoline
	embeddedTrampoline = []byte("stub")
	t.Cleanup(func() { embeddedTrampoline = saved })

	home := t.TempDir()
	p := paths.FromHome(home)
	d := Deps{Env: func(string) string { return "" }, MkdirAll: os.MkdirAll, Stat: os.Stat}

	got, err := ensure("v3.9.0", p, d)
	require.NoError(t, err)
	assert.Equal(t, p.TrampolineBinary("3.9.0"), got)
}

func TestEnsureForVersion_RewritesWhenShaMismatch(t *testing.T) {
	saved := embeddedTrampoline
	embeddedTrampoline = []byte("new-bytes")
	t.Cleanup(func() { embeddedTrampoline = saved })

	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(filepath.Dir(p.TrampolineBinary("9.9.9")), 0o755))
	require.NoError(t, os.WriteFile(p.TrampolineBinary("9.9.9"), []byte("stale-bytes"), 0o755))

	d := Deps{Env: func(string) string { return "" }, MkdirAll: os.MkdirAll, Stat: os.Stat}

	got, err := ensure("9.9.9", p, d)
	require.NoError(t, err)

	body, err := os.ReadFile(got)
	require.NoError(t, err)
	assert.Equal(t, embeddedTrampoline, body, "cache must be rewritten when existing sha does not match embed")

}
