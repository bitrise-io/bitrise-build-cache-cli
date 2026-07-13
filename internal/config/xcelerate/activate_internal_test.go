//go:build unit

package xcelerate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteExecutableAtomically_OverwritesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, cliBasename)
	require.NoError(t, os.WriteFile(target, []byte("OLD-BINARY"), 0o755))

	require.NoError(t, writeExecutableAtomically(dir, target, strings.NewReader("NEW-BINARY")))

	got, err := os.ReadFile(target) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "NEW-BINARY", string(got))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	assertNoTempFilesLeftBehind(t, dir)
}

// Regression for the "corrupted cli left behind" failure: a copy that fails
// partway must not touch the already-installed binary. The old in-place
// O_TRUNC write truncated the target before failing; the atomic rename leaves
// it untouched.
func TestWriteExecutableAtomically_FailedCopyLeavesTargetIntact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, cliBasename)
	require.NoError(t, os.WriteFile(target, []byte("GOOD-INSTALLED-BINARY"), 0o755))

	err := writeExecutableAtomically(dir, target, &failingReader{data: []byte("partial")})
	require.Error(t, err)

	got, err := os.ReadFile(target) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "GOOD-INSTALLED-BINARY", string(got),
		"a failed copy must not corrupt or truncate the installed binary")

	assertNoTempFilesLeftBehind(t, dir)
}

func TestWriteExecutableAtomically_CreatesTargetWhenMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, cliBasename)

	require.NoError(t, writeExecutableAtomically(dir, target, strings.NewReader("FRESH")))

	got, err := os.ReadFile(target) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "FRESH", string(got))

	assertNoTempFilesLeftBehind(t, dir)
}

func assertNoTempFilesLeftBehind(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "temp file left behind: %s", e.Name())
	}
}

type failingReader struct {
	data []byte
	read bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, errors.New("simulated read failure")
	}

	r.read = true
	n := copy(p, r.data)

	return n, nil
}

var _ io.Reader = (*failingReader)(nil)
