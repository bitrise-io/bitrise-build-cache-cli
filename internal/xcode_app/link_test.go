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

const minimalPbxWithBaseRef = `// !$*UTF8*$!
{
	archiveVersion = 1;
	classes = {
	};
	objectVersion = 77;
	objects = {

/* Begin PBXFileReference section */
		AAAAAAAAAAAAAAAAAAAAAAAA /* Base.xcconfig */ = {isa = PBXFileReference; lastKnownFileType = text.xcconfig; path = "Base.xcconfig"; sourceTree = "<group>"; };
/* End PBXFileReference section */

/* Begin XCBuildConfiguration section */
		BBBBBBBBBBBBBBBBBBBBBBBB /* Debug */ = {
			isa = XCBuildConfiguration;
			baseConfigurationReference = AAAAAAAAAAAAAAAAAAAAAAAA /* Base.xcconfig */;
			buildSettings = {
				PRODUCT_NAME = "$(TARGET_NAME)";
			};
			name = Debug;
		};
/* End XCBuildConfiguration section */
	};
}
`

const minimalPbxNoBaseRef = `// !$*UTF8*$!
{
	archiveVersion = 1;
	classes = {
	};
	objectVersion = 77;
	objects = {

/* Begin PBXFileReference section */
/* End PBXFileReference section */

/* Begin XCBuildConfiguration section */
		CCCCCCCCCCCCCCCCCCCCCCCC /* Debug */ = {
			isa = XCBuildConfiguration;
			buildSettings = {
				PRODUCT_NAME = "$(TARGET_NAME)";
			};
			name = Debug;
		};
/* End XCBuildConfiguration section */
	};
}
`

func writeProject(t *testing.T, dir, name, pbxContent string, extraFiles map[string]string) string {
	t.Helper()

	projPath := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(projPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projPath, "project.pbxproj"), []byte(pbxContent), 0o644))

	for relPath, body := range extraFiles {
		full := filepath.Join(dir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}

	return projPath
}

func TestLink_appendsMarkerBlockIdempotently(t *testing.T) {
	tmp := t.TempDir()
	projPath := writeProject(t, tmp, "Sample.xcodeproj", minimalPbxWithBaseRef, map[string]string{
		"Base.xcconfig": "FOO = BAR\n",
	})

	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	result1, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          projPath,
		OverrideXCConfigPath: override,
	})
	require.NoError(t, err)
	require.Len(t, result1.ModifiedXCConfigs, 1)

	body, err := os.ReadFile(result1.ModifiedXCConfigs[0])
	require.NoError(t, err)
	assert.Contains(t, string(body), LinkBlockStart)
	assert.Contains(t, string(body), LinkBlockEnd)
	assert.Contains(t, string(body), `#include? "`+override+`"`)
	assert.Contains(t, string(body), "FOO = BAR", "existing content preserved")

	// Second run must not duplicate the block, and must report no change.
	result2, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          projPath,
		OverrideXCConfigPath: override,
	})
	require.NoError(t, err)
	assert.Empty(t, result2.ModifiedXCConfigs, "idempotent re-run should be a no-op")

	body2, err := os.ReadFile(result1.ModifiedXCConfigs[0])
	require.NoError(t, err)
	assert.Equal(t, string(body), string(body2), "re-run must produce byte-identical content")
	assert.Equal(t, 1, strings.Count(string(body2), LinkBlockStart), "exactly one marker block")
}

func TestUnlink_removesMarkerBlock(t *testing.T) {
	tmp := t.TempDir()
	projPath := writeProject(t, tmp, "Sample.xcodeproj", minimalPbxWithBaseRef, map[string]string{
		"Base.xcconfig": "FOO = BAR\n",
	})
	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath, OverrideXCConfigPath: override})
	require.NoError(t, err)

	result, err := Unlink(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath})
	require.NoError(t, err)
	require.Len(t, result.ModifiedXCConfigs, 1)

	body, err := os.ReadFile(result.ModifiedXCConfigs[0])
	require.NoError(t, err)
	assert.NotContains(t, string(body), LinkBlockStart)
	assert.NotContains(t, string(body), LinkBlockEnd)
	assert.Contains(t, string(body), "FOO = BAR")
}

func TestLink_noBaseConfigCreatesSibling(t *testing.T) {
	tmp := t.TempDir()
	projPath := writeProject(t, tmp, "NoBase.xcodeproj", minimalPbxNoBaseRef, nil)

	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          projPath,
		OverrideXCConfigPath: override,
	})
	require.NoError(t, err)
	require.Len(t, result.CreatedSiblings, 1)

	siblingPath := filepath.Join(tmp, SiblingXCConfigName)
	assert.Equal(t, siblingPath, result.CreatedSiblings[0])

	body, err := os.ReadFile(siblingPath)
	require.NoError(t, err)
	assert.Contains(t, string(body), LinkBlockStart)
	assert.Contains(t, string(body), `#include? "`+override+`"`)

	pbxBody, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)
	pbxStr := string(pbxBody)
	assert.Contains(t, pbxStr, "baseConfigurationReference = ", "pbxproj must gain a baseConfigurationReference")
	assert.Contains(t, pbxStr, SiblingXCConfigName, "pbxproj must reference the sibling by filename")
}

func TestUnlink_removesEmptySibling(t *testing.T) {
	tmp := t.TempDir()
	projPath := writeProject(t, tmp, "NoBase.xcodeproj", minimalPbxNoBaseRef, nil)

	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath, OverrideXCConfigPath: override})
	require.NoError(t, err)

	result, err := Unlink(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath})
	require.NoError(t, err)
	require.Len(t, result.RemovedSiblings, 1)

	_, statErr := os.Stat(filepath.Join(tmp, SiblingXCConfigName))
	assert.True(t, os.IsNotExist(statErr), "sibling xcconfig should be gone")
}

func TestLink_rejectsNonAbsOverride(t *testing.T) {
	tmp := t.TempDir()
	projPath := writeProject(t, tmp, "Sample.xcodeproj", minimalPbxWithBaseRef, map[string]string{
		"Base.xcconfig": "",
	})

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          projPath,
		OverrideXCConfigPath: "relative.xcconfig",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

func TestLink_workspaceWalksProjectRefs(t *testing.T) {
	tmp := t.TempDir()

	proj1 := writeProject(t, tmp, "App.xcodeproj", minimalPbxWithBaseRef, map[string]string{
		"Base.xcconfig": "APP = 1\n",
	})
	_ = proj1

	wsPath := filepath.Join(tmp, "App.xcworkspace")
	require.NoError(t, os.MkdirAll(wsPath, 0o755))
	contents := `<?xml version="1.0" encoding="UTF-8"?>
<Workspace version = "1.0">
  <FileRef location = "group:App.xcodeproj"/>
  <FileRef location = "group:SomePackage/Package.swift"/>
</Workspace>
`
	require.NoError(t, os.WriteFile(filepath.Join(wsPath, "contents.xcworkspacedata"), []byte(contents), 0o644))

	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          wsPath,
		OverrideXCConfigPath: override,
	})
	require.NoError(t, err)
	// Only the .xcodeproj gets touched; Package.swift is skipped by extension filter.
	require.Len(t, result.ModifiedXCConfigs, 1)
}
