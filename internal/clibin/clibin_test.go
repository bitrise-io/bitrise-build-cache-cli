//go:build unit

package clibin

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyToStable(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "bitrise-build-cache")
	contents := []byte("#!/bin/sh\necho test\n")
	require.NoError(t, os.WriteFile(src, contents, 0o600))

	dst, err := CopyToStable(src)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmpHome, ".bitrise", "bin", "bitrise-build-cache"), dst)

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, contents, got)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestCopyToStable_overwritesExisting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "bitrise-build-cache")
	require.NoError(t, os.WriteFile(src, []byte("v2"), 0o600))

	dst := filepath.Join(tmpHome, ".bitrise", "bin", "bitrise-build-cache")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("v1"), 0o755))

	_, err := CopyToStable(src)
	require.NoError(t, err)

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), got)
}

func TestIsTransientPath(t *testing.T) {
	cases := map[string]bool{
		"/tmp/bitrise-build-cache":                  true,
		"/private/tmp/bitrise-build-cache":          true,
		"/var/folders/xy/abc/T/bitrise-build-cache": true,
		// what `go run .` resolves os.Executable() to
		"/var/folders/4h/11t9/T/go-build424/b001/exe/main":  true,
		"/private/var/folders/xy/abc/T/bitrise-build-cache": true,
		"/Users/me/.local/bin/bitrise-build-cache":          false,
		"/opt/homebrew/bin/bitrise-build-cache":             false,
		"/usr/local/bin/bitrise-build-cache":                false,
		"":                                                  false,
	}

	for path, want := range cases {
		assert.Equal(t, want, IsTransientPath(path), "path=%q", path)
	}
}

// Resolve is only interesting when the running binary is somewhere transient,
// which is exactly the state a test binary is in: `go test` builds into the same
// temp tree `go run` does. That makes the bug this guards against reproducible
// here — a config written by `go run . activate bazel` pointed at a path that no
// longer existed by the time Bazel called it.
func TestResolve_TestBinaryIsItselfOnATransientPath(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	require.True(t, IsTransientPath(exe),
		"expected the test binary at %s to look transient; if this fails the two cases below prove nothing", exe)
}

func TestResolve_CopiesOffATransientPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got := Resolve(newTestLogger(), CopyToStableDir)

	stable, err := StablePath()
	require.NoError(t, err)
	// Not asserting the result looks non-transient: t.TempDir() puts HOME under
	// /var/folders on macOS, so the stable path is only stable for a real home.
	assert.Equal(t, stable, got)

	info, err := os.Stat(got)
	require.NoError(t, err, "the pinned path has to exist after this command exits")
	assert.NotZero(t, info.Size())
}

func TestResolve_PreferPATHLeavesTheBareNameToTheCaller(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got := Resolve(newTestLogger(), PreferPATH)

	assert.Empty(t, got, "an empty path is how callers fall back to the $PATH lookup")

	stable, err := StablePath()
	require.NoError(t, err)
	assert.NoFileExists(t, stable, "PreferPATH must not copy anything")
}

func newTestLogger() log.Logger {
	return log.NewLogger(log.WithOutput(io.Discard))
}
