//go:build unit

package clibin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureInstalledInUserLocalBin_SkipsWhenOnPATH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	fakeBin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "bitrise-build-cache"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", fakeBin)

	target, installed, err := EnsureInstalledInUserLocalBin(newTestLogger())
	require.NoError(t, err)
	assert.False(t, installed, "must skip when a bare name resolves on PATH")
	assert.Equal(t, filepath.Join(home, ".local", "bin", "bitrise-build-cache"), target)
	_, statErr := os.Stat(target)
	assert.True(t, os.IsNotExist(statErr), "must NOT create the target when already on PATH")
}

func TestEnsureInstalledInUserLocalBin_CopiesRunningBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	target, installed, err := EnsureInstalledInUserLocalBin(newTestLogger())
	require.NoError(t, err)
	assert.True(t, installed)

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.False(t, info.IsDir())
	assert.NotZero(t, info.Size(), "copied binary should be non-empty")
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestEnsureInstalledInUserLocalBin_SkipsWhenAlreadyAtTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	bin := filepath.Join(home, ".local", "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	// point PATH at the target dir and give it a bitrise-build-cache: OnPATH() will
	// take the fast path, but that means the "already at target" branch also matches
	// (both branches must return installed=false without touching disk).
	require.NoError(t, os.WriteFile(filepath.Join(bin, "bitrise-build-cache"), []byte("existing"), 0o755))
	t.Setenv("PATH", bin)

	target, installed, err := EnsureInstalledInUserLocalBin(newTestLogger())
	require.NoError(t, err)
	assert.False(t, installed)
	assert.Equal(t, filepath.Join(bin, "bitrise-build-cache"), target)

	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "existing", string(content), "must not overwrite the existing binary")
}
