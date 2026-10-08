//go:build unit

package trampoline

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeFileInfo struct {
	mode os.FileMode
}

func (fakeFileInfo) Name() string         { return "" }
func (fakeFileInfo) Size() int64          { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (fakeFileInfo) ModTime() time.Time   { return time.Time{} }
func (fakeFileInfo) IsDir() bool          { return false }
func (fakeFileInfo) Sys() any             { return nil }

func baseDeps(t *testing.T, devDir string) resolveDeps {
	t.Helper()
	tmp := t.TempDir()
	devMtime := time.Unix(1_700_000_000, 0)

	return resolveDeps{
		developerDir: func() string { return devDir },
		stat:         os.Stat,
		xcrun: func(string) (string, error) {
			return "", errors.New("xcrun should not be called in this test")
		},
		xcodeSelectMTS: func() (time.Time, bool) { return devMtime, true },
		readFile:       os.ReadFile,
		writeFile:      writeFileAtomic,
		cacheDir:       func() string { return tmp },
		now:            func() time.Time { return time.Unix(1_700_000_100, 0) },
	}
}

func TestResolveReal_PrimaryHit(t *testing.T) {
	devDir := t.TempDir()
	binDir := filepath.Join(devDir, "Toolchains", "XcodeDefault.xctoolchain", "usr", "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	real := filepath.Join(binDir, "swiftc")
	require.NoError(t, os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755))

	d := baseDeps(t, devDir)
	got, err := resolveReal("swiftc", d)
	require.NoError(t, err)
	assert.Equal(t, real, got)
}

func TestResolveReal_FallsBackToXcrun(t *testing.T) {
	devDir := t.TempDir() // empty, no primary binary
	stub := filepath.Join(t.TempDir(), "swiftc")
	require.NoError(t, os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755))

	d := baseDeps(t, devDir)
	d.xcrun = func(string) (string, error) { return stub, nil }

	got, err := resolveReal("swiftc", d)
	require.NoError(t, err)
	assert.Equal(t, stub, got)
}

func TestResolveReal_CacheHitSkipsXcrun(t *testing.T) {
	devDir := t.TempDir()
	stub := filepath.Join(t.TempDir(), "swiftc")
	require.NoError(t, os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755))

	calls := 0
	d := baseDeps(t, devDir)
	d.xcrun = func(string) (string, error) {
		calls++

		return stub, nil
	}

	first, err := resolveReal("swiftc", d)
	require.NoError(t, err)
	assert.Equal(t, stub, first)
	assert.Equal(t, 1, calls)

	second, err := resolveReal("swiftc", d)
	require.NoError(t, err)
	assert.Equal(t, stub, second)
	assert.Equal(t, 1, calls, "cached entry must suppress a second xcrun call")
}

func TestResolveReal_CacheInvalidatesOnMTimeChange(t *testing.T) {
	devDir := t.TempDir()
	stub := filepath.Join(t.TempDir(), "swiftc")
	require.NoError(t, os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755))

	currentMTime := time.Unix(1_700_000_000, 0)
	calls := 0
	d := baseDeps(t, devDir)
	d.xcodeSelectMTS = func() (time.Time, bool) { return currentMTime, true }
	d.xcrun = func(string) (string, error) {
		calls++

		return stub, nil
	}

	_, err := resolveReal("swiftc", d)
	require.NoError(t, err)
	currentMTime = currentMTime.Add(time.Hour)

	_, err = resolveReal("swiftc", d)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "mtime change must invalidate cache")
}

func TestResolveReal_SelfReferenceIsFatal(t *testing.T) {
	devDir := t.TempDir()
	selfRef := filepath.Join(devDir, "Toolchains", toolchainBundleID, "usr", "bin", "swiftc")
	require.NoError(t, os.MkdirAll(filepath.Dir(selfRef), 0o755))
	require.NoError(t, os.WriteFile(selfRef, []byte("#!/bin/sh\n"), 0o755))

	d := baseDeps(t, devDir)
	d.xcrun = func(string) (string, error) { return selfRef, nil }

	_, err := resolveReal("swiftc", d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-reference")
}

func TestResolveReal_PrimarySelfReferenceSkipsToXcrun(t *testing.T) {
	// Primary path happens to live in our bundle (DEVELOPER_DIR misconfigured);
	// skip it and go to xcrun.
	devDir := filepath.Join(t.TempDir(), "Developer-"+toolchainBundleID)
	primaryBin := filepath.Join(devDir, "Toolchains", "XcodeDefault.xctoolchain", "usr", "bin")
	require.NoError(t, os.MkdirAll(primaryBin, 0o755))
	primary := filepath.Join(primaryBin, "swiftc")
	require.NoError(t, os.WriteFile(primary, []byte("#!/bin/sh\n"), 0o755))

	legit := filepath.Join(t.TempDir(), "swiftc")
	require.NoError(t, os.WriteFile(legit, []byte("#!/bin/sh\n"), 0o755))

	d := baseDeps(t, devDir)
	d.xcrun = func(string) (string, error) { return legit, nil }

	got, err := resolveReal("swiftc", d)
	require.NoError(t, err)
	assert.Equal(t, legit, got)
}

func TestIsSelfReference(t *testing.T) {
	assert.True(t, isSelfReference("/foo/"+toolchainBundleID+"/usr/bin/swiftc"))
	assert.False(t, isSelfReference("/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin/swiftc"))
}

func TestParseCache_RoundTrip(t *testing.T) {
	rec, ok := parseCache([]byte("/path\n1234567890\n"))
	require.True(t, ok)
	assert.Equal(t, "/path", rec.resolvedPath)
	assert.Equal(t, int64(1234567890), rec.devDirMTime.UnixNano())
}
