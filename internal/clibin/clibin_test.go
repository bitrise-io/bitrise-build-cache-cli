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

// $PATH wins even for a binary with a perfectly usable path: an absolute path in
// a generated config goes stale as soon as an upgrade moves the binary.
func TestResolvePath(t *testing.T) {
	const stable = "/usr/local/bin/bitrise-build-cache"
	transient := filepath.Join(os.TempDir(), "go-build123", "b001", "exe", "main")

	cases := []struct {
		name   string
		exe    string
		onPATH bool
		want   string
	}{
		{name: "on PATH wins over a stable path", exe: stable, onPATH: true, want: ""},
		{name: "on PATH wins over a transient path", exe: transient, onPATH: true, want: ""},
		{name: "not on PATH falls back to the real path", exe: stable, onPATH: false, want: stable},
		{name: "not on PATH and transient leaves nothing usable", exe: transient, onPATH: false, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolvePath(tc.exe, tc.onPATH))
		})
	}
}

// End to end through the process state: with the CLI on $PATH, the caller is told
// to use the bare name and nothing is copied anywhere.
func TestResolve_PrefersPATH(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeBin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "bitrise-build-cache"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", fakeBin)

	assert.Empty(t, Resolve(newTestLogger()))

	stable, err := StablePath()
	require.NoError(t, err)
	assert.NoFileExists(t, stable, "resolving must not copy the binary anywhere")
}

func TestResolve_TransientPathWithNothingOnPATH(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // empty dir: nothing answers to the name

	assert.Empty(t, Resolve(newTestLogger()), "no usable path and no $PATH entry")
}

func newTestLogger() log.Logger {
	return log.NewLogger(log.WithOutput(io.Discard))
}
