//go:build unit

package xcode

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs/mocks"
)

func Test_workspaceSHA_deterministic(t *testing.T) {
	a := workspaceSHA("/work/app")
	b := workspaceSHA("/work/app")

	assert.Equal(t, a, b, "same input must produce the same output")
}

func Test_workspaceSHA_differentInputs(t *testing.T) {
	a := workspaceSHA("/work/app")
	b := workspaceSHA("/work/other")

	assert.NotEqual(t, a, b)
}

func Test_workspaceSHA_emptyInput(t *testing.T) {
	// Empty input must be stable and non-panic; just assert determinism.
	a := workspaceSHA("")
	b := workspaceSHA("")

	assert.Equal(t, a, b)
	assert.NotEmpty(t, a, "sha256 of empty produces a non-empty hex string")
}

func Test_workspaceSHA_hexShape(t *testing.T) {
	got := workspaceSHA("/work/app")

	// Code takes the first 8 bytes of sha256 and hex-encodes → 16 lowercase hex chars.
	assert.Len(t, got, 16)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]+$`), got)
}

func newRunnerForResolveTest(argsMock *xcodeargsMocks.XcodeArgsMock, home string, noManagedDD bool) *XcodebuildRunner {
	return &XcodebuildRunner{
		Config:      xcelerate.Config{},
		Metadata:    common.CacheConfigMetadata{},
		Logger:      bundleTestLogger,
		CacheLogger: bundleTestLogger,
		XcodeArgs:   argsMock,
		NoManagedDD: noManagedDD,
		Paths:       paths.FromHome(home),
	}
}

func Test_XcodebuildRunner_resolvePrefixMapPaths_userSuppliedWins(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		ProjectDirFunc:      func() string { return "/work/app" },
		DerivedDataPathFunc: func() string { return "/user/dd" },
		ProjectTempDirFunc:  func() string { return "/user/ptd" },
	}
	r := newRunnerForResolveTest(argsMock, "/h", false)

	got := r.resolvePrefixMapPaths()

	assert.Equal(t, "/work/app", got.ProjectDir)
	assert.Equal(t, "/user/dd", got.DerivedDataPath, "user-supplied DerivedDataPath wins")
	assert.Equal(t, "/user/ptd", got.ProjectTempDir, "user-supplied ProjectTempDir wins")
	// Home comes from os.UserHomeDir(); assert non-empty rather than fighting the OS.
	assert.NotEmpty(t, got.Home)
}

func Test_XcodebuildRunner_resolvePrefixMapPaths_wrapperOwnedWhenUserBlank(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		ProjectDirFunc:      func() string { return "/work/app" },
		DerivedDataPathFunc: func() string { return "" },
		ProjectTempDirFunc:  func() string { return "" },
	}
	r := newRunnerForResolveTest(argsMock, "/h", false)

	got := r.resolvePrefixMapPaths()

	assert.Equal(t, "/work/app", got.ProjectDir)
	sha := workspaceSHA("/work/app")
	assert.Contains(t, got.DerivedDataPath, "/h/.bitrise/cache/xcode-dd/"+sha)
	assert.Contains(t, got.ProjectTempDir, "/h/.bitrise/cache/xcode-ptd/"+sha)
}

func Test_XcodebuildRunner_resolvePrefixMapPaths_noManagedDDSkipsWrapperOwned(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		ProjectDirFunc:      func() string { return "/work/app" },
		DerivedDataPathFunc: func() string { return "" },
		ProjectTempDirFunc:  func() string { return "" },
	}
	r := newRunnerForResolveTest(argsMock, "/h", true)

	got := r.resolvePrefixMapPaths()

	assert.Equal(t, "/work/app", got.ProjectDir)
	assert.Empty(t, got.DerivedDataPath, "NoManagedDD must skip wrapper-owned DerivedDataPath")
	assert.Empty(t, got.ProjectTempDir, "NoManagedDD must skip wrapper-owned ProjectTempDir")
}

func Test_XcodebuildRunner_resolvePrefixMapPaths_emptyProjectDirSkipsWrapperOwned(t *testing.T) {
	argsMock := &xcodeargsMocks.XcodeArgsMock{
		ProjectDirFunc:      func() string { return "" },
		DerivedDataPathFunc: func() string { return "" },
		ProjectTempDirFunc:  func() string { return "" },
	}
	r := newRunnerForResolveTest(argsMock, "/h", false)

	got := r.resolvePrefixMapPaths()

	assert.Empty(t, got.ProjectDir)
	assert.Empty(t, got.DerivedDataPath, "no project dir means no workspace SHA, so no managed dirs")
	assert.Empty(t, got.ProjectTempDir)
}

func Test_XcodebuildRunner_resolvePaths_returnsInjectedPathsWhenSet(t *testing.T) {
	r := &XcodebuildRunner{
		Logger: bundleTestLogger,
		Paths:  paths.FromHome("/injected/home"),
	}

	got := r.resolvePaths()
	assert.Equal(t, "/injected/home", got.Home)
}

func Test_XcodebuildRunner_resolvePaths_fallsBackToDefault(t *testing.T) {
	r := &XcodebuildRunner{
		Logger: bundleTestLogger,
		// Paths.Home is empty → falls back to paths.Default().
	}

	// paths.Default() reads $HOME.
	t.Setenv("HOME", "/tmp/fallback-home")

	got := r.resolvePaths()
	assert.Equal(t, "/tmp/fallback-home", got.Home)
}

func Test_replaceOrAppendBuildSetting_appendsWhenAbsent(t *testing.T) {
	argv := []string{"xcodebuild", "-scheme", "App"}

	out := replaceOrAppendBuildSetting(argv, "OTHER_CFLAGS", "-Wall")

	require.Equal(t, []string{"xcodebuild", "-scheme", "App", "OTHER_CFLAGS=-Wall"}, out)
}

func Test_replaceOrAppendBuildSetting_replacesSingleOccurrence(t *testing.T) {
	argv := []string{"xcodebuild", "OTHER_CFLAGS=$(inherited) -Werror", "-scheme", "App"}

	out := replaceOrAppendBuildSetting(argv, "OTHER_CFLAGS", "-Wall")

	require.Equal(t, []string{"xcodebuild", "OTHER_CFLAGS=-Wall", "-scheme", "App"}, out)
}

func Test_replaceOrAppendBuildSetting_multipleOccurrences_keepsFirstDropsRest(t *testing.T) {
	// Reading the code: first hit is rewritten in place and marks replaced=true;
	// subsequent hits are dropped ("continue"); no re-append at the end.
	argv := []string{
		"OTHER_CFLAGS=old-1",
		"-scheme", "App",
		"OTHER_CFLAGS=old-2",
	}

	out := replaceOrAppendBuildSetting(argv, "OTHER_CFLAGS", "new")

	require.Equal(t, []string{"OTHER_CFLAGS=new", "-scheme", "App"}, out,
		"first occurrence gets replaced in place; later occurrences are dropped")
}

func Test_replaceOrAppendBuildSetting_emptyArgv(t *testing.T) {
	out := replaceOrAppendBuildSetting(nil, "KEY", "value")

	require.Equal(t, []string{"KEY=value"}, out)
}

func Test_replaceOrAppendBuildSetting_valuePreservesEmbeddedEquals(t *testing.T) {
	argv := []string{"xcodebuild"}

	out := replaceOrAppendBuildSetting(argv, "KEY", "a=b=c")

	require.Equal(t, []string{"xcodebuild", "KEY=a=b=c"}, out,
		"only the first '=' separates key from value; the rest is preserved")
}

func Test_writeHandledMarker_createsFileUnderStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeHandledMarker(bundleTestLogger, "inv-abc")

	marker := paths.FromHome(home).XcelerateHandledInvocationFile("inv-abc")
	info, err := os.Stat(marker)
	require.NoError(t, err, "marker file must exist after successful write")
	assert.False(t, info.IsDir())
}

func Test_writeHandledMarker_emptyIDIsNoop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeHandledMarker(bundleTestLogger, "")

	// The dir itself may or may not exist; the assertion is that no marker file was written.
	entries, err := os.ReadDir(paths.FromHome(home).XcelerateHandledInvocationDir())
	if err != nil {
		return // dir absent → definitely no marker written
	}
	assert.Empty(t, entries, "empty invocation ID must not write any marker file")
}

func Test_handledMarkerExists_trueWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.XcelerateHandledInvocationDir(), 0o755))
	require.NoError(t, os.WriteFile(p.XcelerateHandledInvocationFile("inv-1"), nil, 0o644))

	assert.True(t, handledMarkerExists("inv-1"))
	assert.False(t, handledMarkerExists("inv-2"))
	assert.False(t, handledMarkerExists(""))
}

func Test_removeHandledMarker_deletesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.XcelerateHandledInvocationDir(), 0o755))
	marker := p.XcelerateHandledInvocationFile("inv-1")
	require.NoError(t, os.WriteFile(marker, nil, 0o644))

	removeHandledMarker("inv-1")

	_, err := os.Stat(marker)
	assert.True(t, os.IsNotExist(err))
}

func Test_pruneHandledMarkers_removesStaleKeepsFresh(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale")
	fresh := filepath.Join(dir, "fresh")
	require.NoError(t, os.WriteFile(stale, nil, 0o644))
	require.NoError(t, os.WriteFile(fresh, nil, 0o644))

	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	pruneHandledMarkers(dir, 24*time.Hour)

	_, err := os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "stale marker must be removed")
	_, err = os.Stat(fresh)
	assert.NoError(t, err, "fresh marker must survive")
}

func Test_pruneHandledMarkers_missingDirIsNoop(t *testing.T) {
	assert.NotPanics(t, func() {
		pruneHandledMarkers(filepath.Join(t.TempDir(), "does-not-exist"), time.Hour)
	})
}
