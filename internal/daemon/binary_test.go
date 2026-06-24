//go:build unit

package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyCLIToStableBin(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "bitrise-build-cache")
	contents := []byte("#!/bin/sh\necho test\n")
	require.NoError(t, os.WriteFile(src, contents, 0o600))

	dst, err := CopyCLIToStableBin(src)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmpHome, ".bitrise", "bin", "bitrise-build-cache"), dst)

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, contents, got)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestCopyCLIToStableBin_overwritesExisting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "bitrise-build-cache")
	require.NoError(t, os.WriteFile(src, []byte("v2"), 0o600))

	dst := filepath.Join(tmpHome, ".bitrise", "bin", "bitrise-build-cache")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("v1"), 0o755))

	_, err := CopyCLIToStableBin(src)
	require.NoError(t, err)

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), got)
}
