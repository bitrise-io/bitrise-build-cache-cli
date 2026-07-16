//go:build unit

package xcode_app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// tempXcodeproj creates a fake .xcodeproj dir (Xcode-shaped enough for our
// stat + ext checks) and returns its absolute path.
func tempXcodeproj(t *testing.T, parent, name string) string {
	t.Helper()

	pth := filepath.Join(parent, name+".xcodeproj")
	require.NoError(t, os.MkdirAll(pth, 0o755))

	return pth
}

func tempXcworkspace(t *testing.T, parent, name, contentsXML string) string {
	t.Helper()

	pth := filepath.Join(parent, name+".xcworkspace")
	require.NoError(t, os.MkdirAll(pth, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pth, "contents.xcworkspacedata"), []byte(contentsXML), 0o644))

	return pth
}

// writeXCConfig drops a .xcconfig at `dir/name` with the given body.
func writeXCConfig(t *testing.T, dir, name, body string) string {
	t.Helper()

	pth := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(pth, []byte(body), 0o644))

	return pth
}

func linkParams(t *testing.T, project, override string) LinkParams {
	t.Helper()

	return LinkParams{
		ProjectPath:          project,
		OverrideXCConfigPath: override,
		StateDir:             filepath.Join(t.TempDir(), "linked-projects"),
	}
}

func TestLink_appendsIncludeToProjectXCConfigs(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	pathA := writeXCConfig(t, dir, "App.xcconfig", "PRODUCT_NAME = MyApp\n")
	pathB := writeXCConfig(t, dir, "App-release.xcconfig", "CONFIGURATION_BUILD_DIR = /tmp/out\n")

	p := linkParams(t, proj, "/Users/x/.bitrise-xcelerate/xcode-app.xcconfig")
	result, err := Link(utils.DefaultOsProxy{}, p)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{pathA, pathB}, result.ModifiedXCConfigs)
	assert.Empty(t, result.AlreadyLinked)
	assert.Empty(t, result.BridgeFiles)

	// Each file keeps its pre-existing content and gains our trailer.
	for _, xcconfig := range []string{pathA, pathB} {
		body, err := os.ReadFile(xcconfig)
		require.NoError(t, err)
		text := string(body)

		assert.Contains(t, text, trailerCommentLine)
		assert.Contains(t, text, `#include? "/Users/x/.bitrise-xcelerate/xcode-app.xcconfig"`)
	}

	// State file was written; content matches what unlink will read back.
	stateFile := filepath.Join(p.StateDir, paths.LinkedProjectStateFilename(proj))
	raw, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	var st linkedProjectState
	require.NoError(t, json.Unmarshal(raw, &st))
	assert.Equal(t, proj, st.ProjectPath)
	assert.Equal(t, LinkModeInPlace, st.Mode)
	assert.ElementsMatch(t, []string{pathA, pathB}, st.ModifiedXCConfigs)
}

func TestLink_isIdempotentInPlace(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")
	xc := writeXCConfig(t, dir, "App.xcconfig", "PRODUCT_NAME = MyApp\n")

	p := linkParams(t, proj, "/abs/xcode-app.xcconfig")

	_, err := Link(utils.DefaultOsProxy{}, p)
	require.NoError(t, err)

	firstBody, err := os.ReadFile(xc)
	require.NoError(t, err)

	second, err := Link(utils.DefaultOsProxy{}, p)
	require.NoError(t, err)
	assert.Empty(t, second.ModifiedXCConfigs)
	require.Len(t, second.AlreadyLinked, 1)
	assert.Equal(t, xc, second.AlreadyLinked[0])

	// File byte-identical after second run.
	after, err := os.ReadFile(xc)
	require.NoError(t, err)
	assert.Equal(t, string(firstBody), string(after))
}

func TestLink_rewritesTrailerOnOverrideChange(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")
	xc := writeXCConfig(t, dir, "App.xcconfig", "PRODUCT_NAME = MyApp\n")

	pA := linkParams(t, proj, "/abs/original.xcconfig")
	pA.StateDir = filepath.Join(t.TempDir(), "state")
	_, err := Link(utils.DefaultOsProxy{}, pA)
	require.NoError(t, err)

	// Second link with a different override — trailer must be replaced, not
	// stacked; there should be exactly one #include? line in the file.
	pB := pA
	pB.OverrideXCConfigPath = "/abs/new.xcconfig"
	result, err := Link(utils.DefaultOsProxy{}, pB)
	require.NoError(t, err)
	require.Len(t, result.ModifiedXCConfigs, 1)

	body, err := os.ReadFile(xc)
	require.NoError(t, err)
	assert.Contains(t, string(body), `#include? "/abs/new.xcconfig"`)
	assert.NotContains(t, string(body), "original.xcconfig")

	// Exactly one trailer.
	assert.Equal(t, 1, strings.Count(string(body), trailerCommentLine))
}

func TestLink_upgradesLegacyIncludeToOptionalInclude(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	// Pre-existing content already carries the Bitrise trailer but with the
	// non-optional `#include` form (legacy). link must detect and upgrade it.
	body := "PRODUCT_NAME = MyApp\n\n" + trailerCommentLine + "\n#include \"/abs/xcode-app.xcconfig\"\n"
	xc := writeXCConfig(t, dir, "App.xcconfig", body)

	result, err := Link(utils.DefaultOsProxy{}, linkParams(t, proj, "/abs/xcode-app.xcconfig"))
	require.NoError(t, err)

	// Regardless of the AlreadyLinked / ModifiedXCConfigs bookkeeping, the file
	// MUST end up using `#include?` (optional include).
	_ = result
	after, err := os.ReadFile(xc)
	require.NoError(t, err)
	assert.Contains(t, string(after), `#include? "/abs/xcode-app.xcconfig"`)
}

func TestLink_fallsBackToBridgeWhenNoXCConfigs(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	result, err := Link(utils.DefaultOsProxy{}, linkParams(t, proj, "/abs/xcode-app.xcconfig"))
	require.NoError(t, err)

	require.Len(t, result.BridgeFiles, 1)
	assert.Empty(t, result.ModifiedXCConfigs)

	bridge := filepath.Join(dir, BridgeXCConfigName)
	assert.Equal(t, bridge, result.BridgeFiles[0])

	body, err := os.ReadFile(bridge)
	require.NoError(t, err)
	assert.Contains(t, string(body), `#include? "/abs/xcode-app.xcconfig"`)
}

func TestLink_skipsBridgeSiblingFromPriorLink(t *testing.T) {
	// A stale sibling bridge from the old link behaviour is sitting in the dir.
	// The new in-place link should delete the sibling and NOT treat the sibling
	// as a valid xcconfig to append into.
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	staleBridge := writeXCConfig(t, dir, BridgeXCConfigName, "#include? \"/old.xcconfig\"\n")
	realXCConfig := writeXCConfig(t, dir, "App.xcconfig", "PRODUCT_NAME = MyApp\n")

	result, err := Link(utils.DefaultOsProxy{}, linkParams(t, proj, "/abs/xcode-app.xcconfig"))
	require.NoError(t, err)

	// Real xcconfig picked up.
	require.ElementsMatch(t, []string{realXCConfig}, result.ModifiedXCConfigs)
	assert.Empty(t, result.BridgeFiles)

	// Stale sibling deleted.
	_, err = os.Stat(staleBridge)
	assert.True(t, os.IsNotExist(err), "expected stale sibling bridge to be removed")
}

func TestLink_skipsBuildArtefactDirs(t *testing.T) {
	// xcconfigs inside .build/, DerivedData/, Pods/, Carthage/, .git/ must not
	// be touched — those are dep-manager caches / build outputs, editing them
	// would either be pointless or destructive.
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	realXC := writeXCConfig(t, dir, "App.xcconfig", "PRODUCT_NAME = MyApp\n")

	skip := []string{".build", "DerivedData", "Pods", "Carthage", ".git", "node_modules"}
	for _, sub := range skip {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o755))
		writeXCConfig(t, filepath.Join(dir, sub), "junk.xcconfig", "OTHER_LDFLAGS = -junk\n")
	}

	result, err := Link(utils.DefaultOsProxy{}, linkParams(t, proj, "/abs/xcode-app.xcconfig"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{realXC}, result.ModifiedXCConfigs)

	for _, sub := range skip {
		body, err := os.ReadFile(filepath.Join(dir, sub, "junk.xcconfig"))
		require.NoError(t, err)
		assert.NotContains(t, string(body), trailerCommentLine, "expected %s/junk.xcconfig untouched", sub)
	}
}

func TestLink_preservesFilePermissions(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")
	xc := writeXCConfig(t, dir, "App.xcconfig", "PRODUCT_NAME = MyApp\n")
	require.NoError(t, os.Chmod(xc, 0o600))

	_, err := Link(utils.DefaultOsProxy{}, linkParams(t, proj, "/abs/xcode-app.xcconfig"))
	require.NoError(t, err)

	info, err := os.Stat(xc)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestLink_rejectsNonExistentPath(t *testing.T) {
	_, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          filepath.Join(t.TempDir(), "missing.xcodeproj"),
		OverrideXCConfigPath: "/abs/xcode-app.xcconfig",
	})
	require.Error(t, err)
}

func TestLink_rejectsWrongExtension(t *testing.T) {
	dir := t.TempDir()
	junk := filepath.Join(dir, "notaproject.txt")
	require.NoError(t, os.WriteFile(junk, []byte(""), 0o644))

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          junk,
		OverrideXCConfigPath: "/abs/xcode-app.xcconfig",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xcodeproj")
}

func TestLink_rejectsRelativeOverridePath(t *testing.T) {
	proj := tempXcodeproj(t, t.TempDir(), "MyApp")

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          proj,
		OverrideXCConfigPath: "~/foo.xcconfig",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

func TestLink_rejectsEmptyOverridePath(t *testing.T) {
	proj := tempXcodeproj(t, t.TempDir(), "MyApp")

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          proj,
		OverrideXCConfigPath: "",
	})
	require.Error(t, err)
}

func TestLink_xcworkspaceFansOutToReferencedProjects(t *testing.T) {
	root := t.TempDir()

	dirOne := filepath.Join(root, "one")
	dirTwo := filepath.Join(root, "two")
	require.NoError(t, os.MkdirAll(dirOne, 0o755))
	require.NoError(t, os.MkdirAll(dirTwo, 0o755))
	tempXcodeproj(t, dirOne, "AppOne")
	tempXcodeproj(t, dirTwo, "AppTwo")

	// Each project has an xcconfig — should be modified in place.
	xcOne := writeXCConfig(t, dirOne, "AppOne.xcconfig", "// one\n")
	xcTwo := writeXCConfig(t, dirTwo, "AppTwo.xcconfig", "// two\n")

	contents := `<?xml version="1.0" encoding="UTF-8"?>
<Workspace version="1.0">
  <FileRef location="group:one/AppOne.xcodeproj"></FileRef>
  <FileRef location="group:two/AppTwo.xcodeproj"></FileRef>
</Workspace>`
	wsPath := tempXcworkspace(t, root, "MultiApp", contents)

	result, err := Link(utils.DefaultOsProxy{}, linkParams(t, wsPath, "/abs/xcode-app.xcconfig"))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{xcOne, xcTwo}, result.ModifiedXCConfigs)
}

func TestLink_xcworkspaceIgnoresNonXcodeprojFileRefs(t *testing.T) {
	root := t.TempDir()
	tempXcodeproj(t, root, "OnlyOne")

	contents := `<?xml version="1.0" encoding="UTF-8"?>
<Workspace version="1.0">
  <FileRef location="group:OnlyOne.xcodeproj"></FileRef>
  <FileRef location="group:Docs"></FileRef>
</Workspace>`
	wsPath := tempXcworkspace(t, root, "Mixed", contents)

	result, err := Link(utils.DefaultOsProxy{}, linkParams(t, wsPath, "/abs/xcode-app.xcconfig"))
	require.NoError(t, err)
	// No xcconfigs in the project dir → fallback sibling written for OnlyOne.
	require.Len(t, result.BridgeFiles, 1)
}

func TestLink_xcworkspaceEmptyFileRefsIsError(t *testing.T) {
	root := t.TempDir()
	contents := `<?xml version="1.0" encoding="UTF-8"?>
<Workspace version="1.0">
</Workspace>`
	wsPath := tempXcworkspace(t, root, "Empty", contents)

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          wsPath,
		OverrideXCConfigPath: "/abs/xcode-app.xcconfig",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no .xcodeproj")
}

func TestLink_xcworkspaceHandlesGroupLocationPrefix(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "modules")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	tempXcodeproj(t, sub, "Nested")

	contents := `<?xml version="1.0" encoding="UTF-8"?>
<Workspace version="1.0">
  <Group location="group:modules">
    <FileRef location="group:Nested.xcodeproj"></FileRef>
  </Group>
</Workspace>`
	wsPath := tempXcworkspace(t, root, "PrefixJoin", contents)

	result, err := Link(utils.DefaultOsProxy{}, linkParams(t, wsPath, "/abs/xcode-app.xcconfig"))
	require.NoError(t, err)
	// No xcconfigs under modules/ → sibling fallback next to Nested.xcodeproj.
	require.Len(t, result.BridgeFiles, 1)
	assert.True(t, strings.HasSuffix(result.BridgeFiles[0], filepath.Join("modules", BridgeXCConfigName)))
}

func TestLink_xcworkspaceSkipsMissingProjectPaths(t *testing.T) {
	root := t.TempDir()
	tempXcodeproj(t, root, "Real")

	contents := `<?xml version="1.0" encoding="UTF-8"?>
<Workspace version="1.0">
  <FileRef location="group:Real.xcodeproj"></FileRef>
  <FileRef location="group:DoesNotExist.xcodeproj"></FileRef>
</Workspace>`
	wsPath := tempXcworkspace(t, root, "MixedReality", contents)

	result, err := Link(utils.DefaultOsProxy{}, linkParams(t, wsPath, "/abs/xcode-app.xcconfig"))
	require.NoError(t, err)
	// Only the real project is processed. No xcconfigs → sibling.
	require.Len(t, result.BridgeFiles, 1)
}

func TestUnlink_stripsTrailerFromEachXCConfig(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	origA := "PRODUCT_NAME = MyApp\n"
	origB := "CONFIGURATION_BUILD_DIR = /tmp\n"
	pathA := writeXCConfig(t, dir, "App.xcconfig", origA)
	pathB := writeXCConfig(t, dir, "App-release.xcconfig", origB)

	params := linkParams(t, proj, "/abs/xcode-app.xcconfig")
	_, err := Link(utils.DefaultOsProxy{}, params)
	require.NoError(t, err)

	result, err := Unlink(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath: proj,
		StateDir:    params.StateDir,
	})
	require.NoError(t, err)
	assert.False(t, result.NoOp)
	assert.ElementsMatch(t, []string{pathA, pathB}, result.ModifiedXCConfigs)

	// Files restored byte-identical.
	afterA, err := os.ReadFile(pathA)
	require.NoError(t, err)
	assert.Equal(t, origA, string(afterA))

	afterB, err := os.ReadFile(pathB)
	require.NoError(t, err)
	assert.Equal(t, origB, string(afterB))

	// State file cleared.
	stateFile := filepath.Join(params.StateDir, paths.LinkedProjectStateFilename(proj))
	_, statErr := os.Stat(stateFile)
	assert.True(t, os.IsNotExist(statErr))
}

func TestUnlink_removesSiblingBridge(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	// Fallback path: no xcconfigs, sibling gets written.
	params := linkParams(t, proj, "/abs/xcode-app.xcconfig")
	first, err := Link(utils.DefaultOsProxy{}, params)
	require.NoError(t, err)
	require.Len(t, first.BridgeFiles, 1)

	result, err := Unlink(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath: proj,
		StateDir:    params.StateDir,
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{first.BridgeFiles[0]}, result.RemovedBridgeFiles)

	_, statErr := os.Stat(first.BridgeFiles[0])
	assert.True(t, os.IsNotExist(statErr))
}

func TestUnlink_reportsNoOpWhenNothingToDo(t *testing.T) {
	proj := tempXcodeproj(t, t.TempDir(), "MyApp")

	result, err := Unlink(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath: proj,
		StateDir:    filepath.Join(t.TempDir(), "empty"),
	})
	require.NoError(t, err)
	assert.True(t, result.NoOp)
	assert.Empty(t, result.ModifiedXCConfigs)
	assert.Empty(t, result.RemovedBridgeFiles)
}

func TestUnlink_scrapesBridgeWithoutStateFile(t *testing.T) {
	// Simulate a state file wiped by hand but a sibling bridge left behind —
	// unlink must still remove the bridge (defensive sweep).
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")
	bridge := writeXCConfig(t, dir, BridgeXCConfigName, "#include? \"/old.xcconfig\"\n")

	result, err := Unlink(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath: proj,
		StateDir:    filepath.Join(t.TempDir(), "empty"),
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{bridge}, result.RemovedBridgeFiles)
}

func TestUnlink_rejectsNonExistentPath(t *testing.T) {
	_, err := Unlink(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath: filepath.Join(t.TempDir(), "missing.xcodeproj"),
	})
	require.Error(t, err)
}

func TestResolveWorkspaceFileRef_schemes(t *testing.T) {
	const workspace = "/Users/foo/App.xcworkspace"
	cases := []struct {
		name     string
		location string
		wantPath string
		wantOK   bool
	}{
		{
			name:     "self returns the workspace itself",
			location: "self:",
			wantPath: workspace,
			wantOK:   true,
		},
		{
			name:     "group is workspace-relative",
			location: "group:App.xcodeproj",
			wantPath: "/Users/foo/App.xcodeproj",
			wantOK:   true,
		},
		{
			name:     "group with nested path is workspace-relative",
			location: "group:modules/App.xcodeproj",
			wantPath: "/Users/foo/modules/App.xcodeproj",
			wantOK:   true,
		},
		{
			name:     "container is workspace-relative",
			location: "container:App.xcodeproj",
			wantPath: "/Users/foo/App.xcodeproj",
			wantOK:   true,
		},
		{
			name:     "absolute keeps the leading slash",
			location: "absolute:/Users/other/Standalone.xcodeproj",
			wantPath: "/Users/other/Standalone.xcodeproj",
			wantOK:   true,
		},
		{
			name:     "unknown scheme returns ok=false",
			location: "developer:Something.xcodeproj",
			wantPath: "",
			wantOK:   false,
		},
		{
			name:     "malformed location returns ok=false",
			location: "no-scheme",
			wantPath: "",
			wantOK:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveWorkspaceFileRef(workspace, tc.location)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantPath, got)
		})
	}
}

func TestDetectTrailer_ignoresUnrelatedIncludes(t *testing.T) {
	// A user-authored `#include?` that isn't preceded by our exact comment line
	// must not be treated as our trailer. link would otherwise clobber it.
	content := "PRODUCT_NAME = MyApp\n#include? \"/user/managed.xcconfig\"\n"
	has, path, _ := detectTrailer(content)
	assert.False(t, has)
	assert.Empty(t, path)
}

func TestStripTrailer_isNoOpWhenAbsent(t *testing.T) {
	orig := "PRODUCT_NAME = MyApp\n"
	assert.Equal(t, orig, stripTrailerFromContent(orig))
}

func TestAppendTrailer_startsFromCleanFileEndsWithSingleNewline(t *testing.T) {
	// Even when the source has no trailing newline, the trailer starts on a
	// fresh line and the whole file ends with a single newline.
	out := appendTrailerToContent("A = 1", "/abs/xcode-app.xcconfig")
	assert.True(t, strings.HasSuffix(out, "\n"))
	// No double blank line at the join.
	assert.NotContains(t, out, "\n\n\n")
}
