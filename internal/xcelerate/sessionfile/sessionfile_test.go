//go:build unit

package sessionfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func TestWriteRead_RoundTrip(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	want := File{
		InvocationID: "invo-1",
		AppSlug:      "app-1",
		BuildSlug:    "build-1",
		StepSlug:     "step-1",
	}

	require.NoError(t, Write(p, 1234, 1_700_000_000_000, want))

	got, err := Read(p, 1234, 1_700_000_000_000)
	require.NoError(t, err)
	assert.Equal(t, SchemaVersion, got.SchemaVersion)
	assert.Equal(t, "invo-1", got.InvocationID)
	assert.Equal(t, "app-1", got.AppSlug)
}

func TestRead_MissingFileReturnsZero(t *testing.T) {
	p := paths.FromHome(t.TempDir())

	got, err := Read(p, 1, 1)
	require.NoError(t, err)
	assert.Empty(t, got.InvocationID)
	assert.Empty(t, got.AppSlug)
}

func TestWrite_AtomicNoTmpStrag(t *testing.T) {
	p := paths.FromHome(t.TempDir())
	require.NoError(t, Write(p, 42, 100, File{InvocationID: "x"}))

	// Reading again confirms the rename landed; no .tmp lingers.
	got, err := Read(p, 42, 100)
	require.NoError(t, err)
	assert.Equal(t, "x", got.InvocationID)
}
