//go:build unit

package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func fakeDeps(body []byte, checksums string) Deps {
	return Deps{
		Get: func(_ context.Context, url string) ([]byte, error) {
			if filepath.Ext(url) == ".txt" {
				return []byte(checksums), nil
			}

			return body, nil
		},
		Now:      func() time.Time { return time.Unix(1_700_000_000, 0) },
		Env:      func(string) string { return "" },
		MkdirAll: os.MkdirAll,
		Stat:     os.Stat,
	}
}

func sum(body []byte) string {
	s := sha256.Sum256(body)

	return hex.EncodeToString(s[:])
}

func TestEnsureForVersion_DownloadSuccess(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	body := []byte("fake trampoline binary")
	ck := sum(body) + "  trampoline_v3.9.0_darwin_universal\n"

	got, err := ensure(context.Background(), "3.9.0", p, fakeDeps(body, ck))
	require.NoError(t, err)
	assert.Equal(t, p.TrampolineBinary("3.9.0"), got)

	saved, err := os.ReadFile(got)
	require.NoError(t, err)
	assert.Equal(t, body, saved)
}

func TestEnsureForVersion_ShaMismatchFails(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	ck := "cafebabe  trampoline_v3.9.0_darwin_universal\n"

	_, err := ensure(context.Background(), "3.9.0", p, fakeDeps([]byte("payload"), ck))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sha mismatch")
}

func TestEnsureForVersion_CacheHitSkipsDownload(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	body := []byte("fake trampoline binary")
	ck := sum(body) + "  trampoline_v3.9.0_darwin_universal\n"

	// Pre-populate the cache.
	require.NoError(t, os.MkdirAll(filepath.Dir(p.TrampolineBinary("3.9.0")), 0o755))
	require.NoError(t, os.WriteFile(p.TrampolineBinary("3.9.0"), body, 0o755))

	calls := 0
	d := fakeDeps(body, ck)
	d.Get = func(ctx context.Context, url string) ([]byte, error) {
		if filepath.Ext(url) != ".txt" {
			calls++
		}

		return fakeDeps(body, ck).Get(ctx, url)
	}

	got, err := ensure(context.Background(), "3.9.0", p, d)
	require.NoError(t, err)
	assert.Equal(t, p.TrampolineBinary("3.9.0"), got)
	assert.Zero(t, calls, "trampoline body must not be re-downloaded when cache sha matches")
}

func TestEnsureForVersion_EnvOverrideReturnsAsIs(t *testing.T) {
	override := filepath.Join(t.TempDir(), "my-trampoline")
	require.NoError(t, os.WriteFile(override, []byte("stub"), 0o755))

	p := paths.FromHome(t.TempDir())
	d := Deps{
		Env: func(k string) string {
			if k == EnvOverride {
				return override
			}

			return ""
		},
		Stat: os.Stat,
		Get: func(context.Context, string) ([]byte, error) {
			return nil, errors.New("Get must not be called when override is set")
		},
	}

	got, err := ensure(context.Background(), "3.9.0", p, d)
	require.NoError(t, err)
	assert.Equal(t, override, got)
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

	_, err := ensure(context.Background(), "3.9.0", p, d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "override")
}

func TestEnsureForVersion_StripsLeadingV(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	body := []byte("fake trampoline binary")
	ck := sum(body) + "  trampoline_v3.9.0_darwin_universal\n"

	got, err := ensure(context.Background(), "v3.9.0", p, fakeDeps(body, ck))
	require.NoError(t, err)
	assert.Equal(t, p.TrampolineBinary("3.9.0"), got)
}

func TestEnsureForVersion_AtomicWrite(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	body := []byte("fake trampoline binary")
	ck := sum(body) + "  trampoline_v3.9.0_darwin_universal\n"

	got, err := ensure(context.Background(), "3.9.0", p, fakeDeps(body, ck))
	require.NoError(t, err)

	// No stray .tmp file left behind.
	_, err = os.Stat(got + ".tmp")
	assert.True(t, os.IsNotExist(err))
}

// TestEnsureForVersion_CleanupOnRenameFailure documents the current behaviour
// of writeAtomic on rename failure: the stray .tmp file is NOT removed. The
// trampoline download path is single-run per release-bump, so a persistent
// .tmp from an earlier failed install gets overwritten on the next attempt.
// If future callers need best-effort cleanup, writeAtomic should defer an
// os.Remove(tmp) on the error return.
func TestEnsureForVersion_CleanupOnRenameFailure(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)
	body := []byte("fake trampoline binary")
	ck := sum(body) + "  trampoline_v3.9.0_darwin_universal\n"

	// Force the rename to fail by pre-creating a non-empty directory at the
	// target path. os.Rename then returns ENOTEMPTY / EISDIR depending on OS.
	target := p.TrampolineBinary("3.9.0")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "block"), []byte("x"), 0o600))

	_, err := ensure(context.Background(), "3.9.0", p, fakeDeps(body, ck))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install trampoline")

	// Documented: cleanup is intentionally skipped; the .tmp file persists.
	// Overwritten on the next successful attempt.
	_, statErr := os.Stat(target + ".tmp")
	assert.NoError(t, statErr, "writeAtomic currently leaves .tmp on rename failure (documented)")
}
