//go:build unit

package toolchain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func seedStockToolchain(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "usr", "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))

	for _, name := range []string{"dsymutil", "clang", "swift", "ld"} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	}

	for _, d := range []string{"usr/lib", "usr/libexec", "Developer"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}

	require.NoError(t, os.WriteFile(filepath.Join(root, "Info.plist"), []byte("stock"), 0o644))

	return root
}

func TestRequire_StagesFarmAndSymlinksEverythingButDsymutil(t *testing.T) {
	home := t.TempDir()
	stock := seedStockToolchain(t)
	p := paths.FromHome(home)

	s, err := Require(context.Background(), Params{
		Paths:             p,
		OsProxy:           utils.DefaultOsProxy{},
		Logger:            log.NewLogger(),
		CLIPath:           "/opt/bin/bitrise-build-cache-cli",
		StockToolchainDir: stock,
		XcodeVersionProbe: func(_ context.Context) (string, error) { return "99A1", nil },
	})
	require.NoError(t, err)
	assert.Equal(t, "99A1", s.XcodeBuildNumber)

	// Shim is a real file (not symlink).
	info, err := os.Lstat(p.DsymutilCasShimPath())
	require.NoError(t, err)
	assert.False(t, info.Mode()&os.ModeSymlink != 0, "dsymutil shim must be a real file, got %s", info.Mode())

	data, err := os.ReadFile(p.DsymutilCasShimPath())
	require.NoError(t, err)
	assert.Contains(t, string(data), "/opt/bin/bitrise-build-cache-cli")
	assert.Contains(t, string(data), "xcelerate dsymutil-shim --")

	// Siblings are symlinks into the stock toolchain.
	for _, name := range []string{"clang", "swift", "ld"} {
		target, err := os.Readlink(filepath.Join(p.DsymutilCasShimToolchainBinDir(), name))
		require.NoError(t, err, "sibling %s must be a symlink", name)
		assert.Equal(t, filepath.Join(stock, "usr", "bin", name), target)
	}

	// Stamp written and matches.
	stampData, err := os.ReadFile(p.DsymutilCasShimToolchainStampFile())
	require.NoError(t, err)
	var stamp Stamp
	require.NoError(t, json.Unmarshal(stampData, &stamp))
	assert.Equal(t, "99A1", stamp.XcodeBuildNumber)

	// User Toolchains symlink points at the farm.
	target, err := os.Readlink(p.DsymutilCasShimUserToolchainSymlink())
	require.NoError(t, err)
	assert.Equal(t, p.DsymutilCasShimToolchainStagingDir(), target)
}

func TestRequire_IsIdempotentOnMatchingStamp(t *testing.T) {
	home := t.TempDir()
	stock := seedStockToolchain(t)
	p := paths.FromHome(home)

	// First stage.
	_, err := Require(context.Background(), Params{
		Paths:             p,
		OsProxy:           utils.DefaultOsProxy{},
		Logger:            log.NewLogger(),
		CLIPath:           "/opt/cli",
		StockToolchainDir: stock,
		XcodeVersionProbe: func(_ context.Context) (string, error) { return "99A1", nil },
	})
	require.NoError(t, err)

	// Mark the shim with a specific mtime so we can detect a rewrite.
	before, err := os.Stat(p.DsymutilCasShimPath())
	require.NoError(t, err)
	wantMtime := before.ModTime()

	// Second stage with identical stamp — must not touch files.
	_, err = Require(context.Background(), Params{
		Paths:             p,
		OsProxy:           utils.DefaultOsProxy{},
		Logger:            log.NewLogger(),
		CLIPath:           "/opt/cli",
		StockToolchainDir: stock,
		XcodeVersionProbe: func(_ context.Context) (string, error) { return "99A1", nil },
	})
	require.NoError(t, err)

	after, err := os.Stat(p.DsymutilCasShimPath())
	require.NoError(t, err)
	assert.Equal(t, wantMtime, after.ModTime(), "shim must not be rewritten on matching stamp")
}

func TestRequire_RestagesOnXcodeBump(t *testing.T) {
	home := t.TempDir()
	stock := seedStockToolchain(t)
	p := paths.FromHome(home)

	_, err := Require(context.Background(), Params{
		Paths:             p,
		OsProxy:           utils.DefaultOsProxy{},
		Logger:            log.NewLogger(),
		CLIPath:           "/opt/cli",
		StockToolchainDir: stock,
		XcodeVersionProbe: func(_ context.Context) (string, error) { return "99A1", nil },
	})
	require.NoError(t, err)

	_, err = Require(context.Background(), Params{
		Paths:             p,
		OsProxy:           utils.DefaultOsProxy{},
		Logger:            log.NewLogger(),
		CLIPath:           "/opt/cli",
		StockToolchainDir: stock,
		XcodeVersionProbe: func(_ context.Context) (string, error) { return "99A2", nil },
	})
	require.NoError(t, err)

	stampData, err := os.ReadFile(p.DsymutilCasShimToolchainStampFile())
	require.NoError(t, err)
	var stamp Stamp
	require.NoError(t, json.Unmarshal(stampData, &stamp))
	assert.Equal(t, "99A2", stamp.XcodeBuildNumber)
}

func TestRemove_IsIdempotent(t *testing.T) {
	home := t.TempDir()
	p := paths.FromHome(home)

	// Nothing staged: Remove is a no-op.
	require.NoError(t, Remove(utils.DefaultOsProxy{}, p))

	stock := seedStockToolchain(t)
	_, err := Require(context.Background(), Params{
		Paths:             p,
		OsProxy:           utils.DefaultOsProxy{},
		Logger:            log.NewLogger(),
		CLIPath:           "/opt/cli",
		StockToolchainDir: stock,
		XcodeVersionProbe: func(_ context.Context) (string, error) { return "99A1", nil },
	})
	require.NoError(t, err)

	require.NoError(t, Remove(utils.DefaultOsProxy{}, p))

	_, err = os.Lstat(p.DsymutilCasShimUserToolchainSymlink())
	assert.True(t, os.IsNotExist(err), "symlink should be gone, got: %v", err)
	_, err = os.Lstat(p.DsymutilCasShimToolchainStagingDir())
	assert.True(t, os.IsNotExist(err), "farm dir should be gone, got: %v", err)
}
