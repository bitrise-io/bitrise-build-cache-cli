//go:build unit

package wrapperless

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDerivedDataRoots_DefaultOnly(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "")
	roots := derivedDataRoots(t.TempDir())

	assert.Contains(t, roots[0], "Library/Developer/Xcode/DerivedData")
}

func TestDerivedDataRoots_EnvOverride_IncludesExtraPaths(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "/tmp/alt-dd:/Volumes/Big/DD")
	roots := derivedDataRoots(t.TempDir())

	assert.Contains(t, roots, "/tmp/alt-dd")
	assert.Contains(t, roots, "/Volumes/Big/DD")
}

func TestDerivedDataRoots_EnvOverride_RejectsRelativePaths(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "./rel:not-abs")
	roots := derivedDataRoots(t.TempDir())

	assert.NotContains(t, roots, "./rel")
	assert.NotContains(t, roots, "not-abs")
}

func TestDerivedDataRoots_EnvOverride_Deduplicates(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "/tmp/dd:/tmp/dd")
	roots := derivedDataRoots(t.TempDir())

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
	// Hide the real home so the OS fallback does not pull the current user's
	// DD into the result.
	t.Setenv("HOME", "")
	roots := derivedDataRoots("")

	assert.Contains(t, roots, "/tmp/only")
}

// TestDerivedDataRoots_HonorsXcodePref writes a com.apple.dt.Xcode.plist under
// a fake home and asserts the custom DerivedData location is included. This is
// the in-process replacement for the previous `defaults read` subprocess.
func TestDerivedDataRoots_HonorsXcodePref(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "")

	fakeHome := t.TempDir()
	plistDir := filepath.Join(fakeHome, "Library", "Preferences")
	require.NoError(t, os.MkdirAll(plistDir, 0o755))
	plistBody := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>IDECustomDerivedDataLocation</key><string>/Volumes/CustomDD</string>
</dict></plist>`
	require.NoError(t, os.WriteFile(filepath.Join(plistDir, "com.apple.dt.Xcode.plist"), []byte(plistBody), 0o644))

	roots := derivedDataRoots(fakeHome)

	assert.Contains(t, roots, "/Volumes/CustomDD")
}

// TestDerivedDataRoots_NoXcodePref asserts the custom location is absent when
// the plist is missing — only the default ~/Library root remains.
func TestDerivedDataRoots_NoXcodePref(t *testing.T) {
	t.Setenv(EnvDerivedDataDirs, "")
	fakeHome := t.TempDir()

	roots := derivedDataRoots(fakeHome)

	assert.Len(t, roots, 1)
	assert.Equal(t, filepath.Join(fakeHome, "Library", "Developer", "Xcode", "DerivedData"), roots[0])
}
