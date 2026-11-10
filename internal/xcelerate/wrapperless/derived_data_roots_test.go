//go:build unit

package wrapperless

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDerivedDataRoots_DefaultOnly(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "")
	roots := derivedDataRoots(context.Background(), "/Users/alice")

	assert.Contains(t, roots, filepath.Join("/Users/alice", "Library", "Developer", "Xcode", "DerivedData"))
}

func TestDerivedDataRoots_EnvOverride_IncludesExtraPaths(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "/tmp/alt-dd:/Volumes/Big/DD")
	roots := derivedDataRoots(context.Background(), "/Users/alice")

	assert.Contains(t, roots, "/tmp/alt-dd")
	assert.Contains(t, roots, "/Volumes/Big/DD")
	assert.Contains(t, roots, filepath.Join("/Users/alice", "Library", "Developer", "Xcode", "DerivedData"))
}

func TestDerivedDataRoots_EnvOverride_RejectsRelativePaths(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "./rel:not-abs")
	roots := derivedDataRoots(context.Background(), "/Users/alice")

	assert.NotContains(t, roots, "./rel")
	assert.NotContains(t, roots, "not-abs")
}

func TestDerivedDataRoots_EnvOverride_Deduplicates(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "/tmp/dd:/tmp/dd")
	roots := derivedDataRoots(context.Background(), "/Users/alice")

	count := 0
	for _, r := range roots {
		if r == "/tmp/dd" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestDerivedDataRoots_EmptyHome_StillReturnsEnvPaths(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "/tmp/only")
	roots := derivedDataRoots(context.Background(), "")

	assert.Contains(t, roots, "/tmp/only")
}
