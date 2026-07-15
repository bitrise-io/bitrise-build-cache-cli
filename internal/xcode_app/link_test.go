//go:build unit

package xcode_app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestLink_writesBridgeNextToXcodeproj(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          proj,
		OverrideXCConfigPath: "/Users/x/.bitrise-xcelerate/xcode-app.xcconfig",
	})
	require.NoError(t, err)

	require.Len(t, result.BridgeFiles, 1)
	assert.Empty(t, result.AlreadyLinked)

	bridgePath := filepath.Join(dir, BridgeXCConfigName)
	assert.Equal(t, bridgePath, result.BridgeFiles[0])

	content, err := os.ReadFile(bridgePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), `#include "/Users/x/.bitrise-xcelerate/xcode-app.xcconfig"`)
}

func TestLink_isIdempotent(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	params := LinkParams{
		ProjectPath:          proj,
		OverrideXCConfigPath: "/abs/xcode-app.xcconfig",
	}

	first, err := Link(utils.DefaultOsProxy{}, params)
	require.NoError(t, err)
	require.Len(t, first.BridgeFiles, 1)

	second, err := Link(utils.DefaultOsProxy{}, params)
	require.NoError(t, err)
	assert.Empty(t, second.BridgeFiles)
	require.Len(t, second.AlreadyLinked, 1)
	assert.Equal(t, first.BridgeFiles[0], second.AlreadyLinked[0])
}

func TestLink_rewritesWhenContentDiffers(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          proj,
		OverrideXCConfigPath: "/abs/original.xcconfig",
	})
	require.NoError(t, err)

	// Now link with a different override; must rewrite.
	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          proj,
		OverrideXCConfigPath: "/abs/new.xcconfig",
	})
	require.NoError(t, err)
	require.Len(t, result.BridgeFiles, 1)

	content, err := os.ReadFile(result.BridgeFiles[0])
	require.NoError(t, err)
	assert.Contains(t, string(content), `#include "/abs/new.xcconfig"`)
	assert.NotContains(t, string(content), "original.xcconfig")
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

	// Two projects under sibling subdirs. Bridges go next to each .xcodeproj,
	// so two distinct parent dirs = two distinct bridge files.
	dirOne := filepath.Join(root, "one")
	dirTwo := filepath.Join(root, "two")
	require.NoError(t, os.MkdirAll(dirOne, 0o755))
	require.NoError(t, os.MkdirAll(dirTwo, 0o755))
	tempXcodeproj(t, dirOne, "AppOne")
	tempXcodeproj(t, dirTwo, "AppTwo")

	contents := `<?xml version="1.0" encoding="UTF-8"?>
<Workspace version="1.0">
  <FileRef location="group:one/AppOne.xcodeproj"></FileRef>
  <FileRef location="group:two/AppTwo.xcodeproj"></FileRef>
</Workspace>`
	wsPath := tempXcworkspace(t, root, "MultiApp", contents)

	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          wsPath,
		OverrideXCConfigPath: "/abs/xcode-app.xcconfig",
	})
	require.NoError(t, err)
	require.Len(t, result.BridgeFiles, 2)

	for _, b := range result.BridgeFiles {
		_, err := os.Stat(b)
		require.NoError(t, err, "expected bridge %s to exist", b)
	}
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

	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          wsPath,
		OverrideXCConfigPath: "/abs/xcode-app.xcconfig",
	})
	require.NoError(t, err)
	assert.Len(t, result.BridgeFiles, 1)
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

func TestLink_xcworkspaceHandlesFileRefsNestedUnderGroup(t *testing.T) {
	// Real workspaces (esp. hand-edited ones) can nest FileRef entries under a
	// <Group>. FileRef location is workspace-relative regardless of the
	// wrapping <Group>, so a full relative path in `group:<path>` works.
	root := t.TempDir()
	sub := filepath.Join(root, "modules")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	tempXcodeproj(t, sub, "Nested")

	contents := `<?xml version="1.0" encoding="UTF-8"?>
<Workspace version="1.0">
  <Group location="group:modules">
    <FileRef location="group:modules/Nested.xcodeproj"></FileRef>
  </Group>
</Workspace>`
	wsPath := tempXcworkspace(t, root, "Grouped", contents)

	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          wsPath,
		OverrideXCConfigPath: "/abs/xcode-app.xcconfig",
	})
	require.NoError(t, err)
	require.Len(t, result.BridgeFiles, 1)
	assert.True(t, strings.HasSuffix(result.BridgeFiles[0], filepath.Join("modules", BridgeXCConfigName)))
}

func TestUnlink_removesBridgeFile(t *testing.T) {
	dir := t.TempDir()
	proj := tempXcodeproj(t, dir, "MyApp")

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          proj,
		OverrideXCConfigPath: "/abs/xcode-app.xcconfig",
	})
	require.NoError(t, err)

	result, err := Unlink(utils.DefaultOsProxy{}, LinkParams{ProjectPath: proj})
	require.NoError(t, err)
	require.Len(t, result.RemovedBridgeFiles, 1)
	assert.Empty(t, result.MissingBridgeFiles)

	_, statErr := os.Stat(result.RemovedBridgeFiles[0])
	assert.True(t, os.IsNotExist(statErr))
}

func TestUnlink_missingBridgeIsNotAnError(t *testing.T) {
	proj := tempXcodeproj(t, t.TempDir(), "MyApp")

	result, err := Unlink(utils.DefaultOsProxy{}, LinkParams{ProjectPath: proj})
	require.NoError(t, err)
	assert.Empty(t, result.RemovedBridgeFiles)
	require.Len(t, result.MissingBridgeFiles, 1)
}

func TestUnlink_rejectsNonExistentPath(t *testing.T) {
	_, err := Unlink(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath: filepath.Join(t.TempDir(), "missing.xcodeproj"),
	})
	require.Error(t, err)
}
