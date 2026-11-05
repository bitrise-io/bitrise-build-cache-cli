//go:build unit

package deriveddata_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
)

const hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const hashC = "cccccccccccccccccccccccccccccccc"

func writeXCBuildData(t *testing.T, dd, workspaceSha, indexPrefix, hashHex string, mtime time.Time) {
	t.Helper()

	dir := filepath.Join(dd, workspaceSha, indexPrefix, "Build/Intermediates.noindex/XCBuildData", hashHex+".xcbuilddata")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.Chtimes(dir, mtime, mtime))
}

func TestFinder_CustomRoot_AbsentPlistReturnsFalse(t *testing.T) {
	home := t.TempDir()
	f := deriveddata.Finder{} //nolint:exhaustruct

	root, ok := f.CustomRoot(home)
	assert.False(t, ok)
	assert.Empty(t, root)
}

func TestFinder_CustomRoot_ReadsKeyWhenPresent(t *testing.T) {
	home := t.TempDir()

	plistPath := filepath.Join(home, "Library/Preferences/com.apple.dt.Xcode.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))

	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>IDECustomDerivedDataLocation</key><string>/Users/me/CustomDD</string>
</dict></plist>`
	require.NoError(t, os.WriteFile(plistPath, []byte(body), 0o644))

	f := deriveddata.Finder{} //nolint:exhaustruct
	root, ok := f.CustomRoot(home)
	assert.True(t, ok)
	assert.Equal(t, "/Users/me/CustomDD", root)
}

func TestFinder_CustomRoot_EmptyValueReturnsFalse(t *testing.T) {
	home := t.TempDir()

	plistPath := filepath.Join(home, "Library/Preferences/com.apple.dt.Xcode.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))

	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>IDECustomDerivedDataLocation</key><string></string>
</dict></plist>`
	require.NoError(t, os.WriteFile(plistPath, []byte(body), 0o644))

	f := deriveddata.Finder{} //nolint:exhaustruct
	root, ok := f.CustomRoot(home)
	assert.False(t, ok)
	assert.Empty(t, root)
}

func TestNewestBuildRequestHash_EmptyRoot(t *testing.T) {
	_, _, ok := deriveddata.NewestBuildRequestHash("", "", time.Now())
	assert.False(t, ok)
}

func TestNewestBuildRequestHash_NoEntriesReturnsFalse(t *testing.T) {
	root := t.TempDir()
	_, _, ok := deriveddata.NewestBuildRequestHash(root, "", time.Now())
	assert.False(t, ok)
}

func TestNewestBuildRequestHash_PicksNewestAcrossSubdirs(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)

	writeXCBuildData(t, root, "App-abc", "", hashA, now.Add(-2*time.Minute))
	writeXCBuildData(t, root, "App-abc", "", hashB, now.Add(-30*time.Second))
	writeXCBuildData(t, root, "Other-def", "", hashC, now.Add(-1*time.Minute))

	hash, mtime, ok := deriveddata.NewestBuildRequestHash(root, "", now)
	require.True(t, ok)
	assert.Equal(t, hashB, hash)
	assert.Equal(t, now.Add(-30*time.Second), mtime)
}

func TestNewestBuildRequestHash_IndexGlobAlsoMatches(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)

	// Only an Index.noindex entry — must still be found and win.
	writeXCBuildData(t, root, "App-abc", "Index.noindex", hashA, now.Add(-5*time.Second))

	hash, _, ok := deriveddata.NewestBuildRequestHash(root, "", now)
	require.True(t, ok)
	assert.Equal(t, hashA, hash)
}

func TestNewestBuildRequestHash_IgnoresNonHexBasename(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)

	// Non-hex basename like a Script-Phase marker must be skipped.
	ddDir := filepath.Join(root, "App-abc", "Build/Intermediates.noindex/XCBuildData", "Script-ABCD.xcbuilddata")
	require.NoError(t, os.MkdirAll(ddDir, 0o755))
	require.NoError(t, os.Chtimes(ddDir, now, now))

	_, _, ok := deriveddata.NewestBuildRequestHash(root, "", now)
	assert.False(t, ok, "non-hex basename must not count as a build-request hash")
}

func TestNewestBuildRequestHash_FutureMtimeClampedToNow(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)

	writeXCBuildData(t, root, "App-abc", "", hashA, now.Add(1*time.Hour))

	hash, mtime, ok := deriveddata.NewestBuildRequestHash(root, "", now)
	require.True(t, ok)
	assert.Equal(t, hashA, hash)
	assert.Equal(t, now, mtime, "future mtime must clamp to now")
}

func TestNewestBuildRequestHash_WorkspaceHintFilters(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)

	writeXCBuildData(t, root, "App-abc", "", hashA, now.Add(-30*time.Second))
	// Newer entry under a different workspace prefix — must be hidden by the hint.
	writeXCBuildData(t, root, "Other-def", "", hashB, now.Add(-10*time.Second))

	hash, _, ok := deriveddata.NewestBuildRequestHash(root, "App-", now)
	require.True(t, ok)
	assert.Equal(t, hashA, hash)
}

func TestNewestBuildRequestEntry_ReturnsDirPath(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)

	writeXCBuildData(t, root, "App-abc", "", hashA, now.Add(-30*time.Second))

	hex, dir, _, ok := deriveddata.NewestBuildRequestEntry(root, "", now)
	require.True(t, ok)
	assert.Equal(t, hashA, hex)
	assert.NotEmpty(t, dir, "dir path must be populated so callers can ReadBuildRequest")
	// The returned dir is the <hex>.xcbuilddata path itself.
	fi, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
}

func TestReadBuildRequest_IndexBuild(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, "testdata/build-request/indexbuild.json", filepath.Join(dir, "build-request.json"))

	br, ok := deriveddata.ReadBuildRequest(dir)
	require.True(t, ok)
	assert.Equal(t, "prepareForIndexing", br.Command)
	assert.Equal(t, "indexbuild", br.Action)
	assert.Equal(t, "Debug", br.Configuration)
	assert.Equal(t, "IceCubesApp", br.Container)
}

func TestReadBuildRequest_Build(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, "testdata/build-request/build.json", filepath.Join(dir, "build-request.json"))

	br, ok := deriveddata.ReadBuildRequest(dir)
	require.True(t, ok)
	assert.Equal(t, "build", br.Command)
	assert.Equal(t, "build", br.Action)
	assert.Equal(t, "Debug", br.Configuration)
	assert.Equal(t, "MyApp", br.Container, "`.xcworkspace` suffix must be stripped from the container basename")
}

func TestReadBuildRequest_Clean(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, "testdata/build-request/clean.json", filepath.Join(dir, "build-request.json"))

	br, ok := deriveddata.ReadBuildRequest(dir)
	require.True(t, ok)
	assert.Equal(t, "clean", br.Command)
	assert.Equal(t, "clean", br.Action)
	assert.Equal(t, "Release", br.Configuration)
	assert.Equal(t, "MyApp", br.Container)
}

func TestReadBuildRequest_MissingFile(t *testing.T) {
	br, ok := deriveddata.ReadBuildRequest(t.TempDir())
	assert.False(t, ok)
	assert.True(t, br.IsEmpty())
}

func TestReadBuildRequest_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build-request.json"), []byte("{not-json"), 0o600))

	br, ok := deriveddata.ReadBuildRequest(dir)
	assert.False(t, ok)
	assert.True(t, br.IsEmpty())
}

func TestReadBuildRequest_PartialSchema(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, "testdata/build-request/action-only.json", filepath.Join(dir, "build-request.json"))

	br, ok := deriveddata.ReadBuildRequest(dir)
	require.True(t, ok, "partial schema must still decode successfully")
	assert.Empty(t, br.Command)
	assert.Equal(t, "indexbuild", br.Action)
	assert.Empty(t, br.Configuration)
	assert.Empty(t, br.Container)
}

func TestReadBuildRequest_StripsContainerSuffix(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, "testdata/build-request/workspace-container.json", filepath.Join(dir, "build-request.json"))

	br, ok := deriveddata.ReadBuildRequest(dir)
	require.True(t, ok)
	assert.Equal(t, "MyApp", br.Container, "containerPath basename with .xcworkspace suffix must collapse to bare project name")
}

func TestReadBuildRequest_EmptyDir(t *testing.T) {
	_, ok := deriveddata.ReadBuildRequest("")
	assert.False(t, ok)
}

// copyFixture reads a fixture file and writes it to dst. Keeps fixtures as
// standalone .json so the real-world shape is visible outside Go source.
func copyFixture(t *testing.T, src, dst string) {
	t.Helper()

	body, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, body, 0o600))
}
