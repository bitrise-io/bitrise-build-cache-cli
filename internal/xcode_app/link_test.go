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

func TestLink_relinkReusesExistingSiblingFileRef(t *testing.T) {
	tmp := t.TempDir()
	projPath := writeProject(t, tmp, "NoBase.xcodeproj", minimalPbxNoBaseRef, nil)

	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath, OverrideXCConfigPath: override})
	require.NoError(t, err)

	pbxAfterFirst, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)

	cfgs := parseBuildConfigurations(string(pbxAfterFirst))
	require.Len(t, cfgs, 1)
	require.NotEmpty(t, cfgs[0].BaseConfigRefID)
	stripped := strings.Replace(string(pbxAfterFirst),
		"baseConfigurationReference = "+cfgs[0].BaseConfigRefID+` /* `+SiblingXCConfigName+" */;\n\t\t\t", "", 1)
	require.NotEqual(t, string(pbxAfterFirst), stripped)
	require.NoError(t, os.WriteFile(filepath.Join(projPath, "project.pbxproj"), []byte(stripped), 0o644))

	_, err = Link(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath, OverrideXCConfigPath: override})
	require.NoError(t, err)

	pbxAfterSecond, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(string(pbxAfterSecond), `path = "`+SiblingXCConfigName+`"`),
		"second Link() must not insert a duplicate sibling PBXFileReference")
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
	// Package.swift is skipped by the extension filter.
	require.Len(t, result.ModifiedXCConfigs, 1)
}

// TestLink_acceptsLegacyAndLowercaseObjectIDs pins the widened pbxproj object-id
// regex: Xcode has shipped both the modern 24-char and the legacy 12-char
// shapes, in lowercase and uppercase hex.
func TestLink_acceptsLegacyAndLowercaseObjectIDs(t *testing.T) {
	tests := []struct {
		name      string
		fileRefID string
		cfgID     string
	}{
		{name: "lowercase 24-char", fileRefID: "aaaaaaaaaaaaaaaaaaaaaaaa", cfgID: "bbbbbbbbbbbbbbbbbbbbbbbb"},
		{name: "mixed-case 24-char", fileRefID: "AaAaAaAaAaAaAaAaAaAaAaAa", cfgID: "BbBbBbBbBbBbBbBbBbBbBbBb"},
		{name: "legacy 12-char uppercase", fileRefID: "AAAAAAAAAAAA", cfgID: "BBBBBBBBBBBB"},
		{name: "legacy 12-char lowercase", fileRefID: "aaaaaaaaaaaa", cfgID: "bbbbbbbbbbbb"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			pbx := `// !$*UTF8*$!
{
	objects = {
/* Begin PBXFileReference section */
		` + tc.fileRefID + ` /* Base.xcconfig */ = {isa = PBXFileReference; lastKnownFileType = text.xcconfig; path = "Base.xcconfig"; sourceTree = "<group>"; };
/* End PBXFileReference section */
/* Begin XCBuildConfiguration section */
		` + tc.cfgID + ` /* Debug */ = {
			isa = XCBuildConfiguration;
			baseConfigurationReference = ` + tc.fileRefID + ` /* Base.xcconfig */;
			buildSettings = {};
			name = Debug;
		};
/* End XCBuildConfiguration section */
	};
}
`
			projPath := writeProject(t, tmp, "Sample.xcodeproj", pbx, map[string]string{
				"Base.xcconfig": "FOO = BAR\n",
			})

			override := filepath.Join(tmp, "override.xcconfig")
			require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

			result, err := Link(utils.DefaultOsProxy{}, LinkParams{
				ProjectPath:          projPath,
				OverrideXCConfigPath: override,
			})
			require.NoError(t, err)
			require.Len(t, result.ModifiedXCConfigs, 1,
				"widened regex must find the base xcconfig for %s", tc.name)
			require.Empty(t, result.CreatedSiblings,
				"must not fall back to sibling path when the base reference is parseable")

			body, err := os.ReadFile(result.ModifiedXCConfigs[0])
			require.NoError(t, err)
			assert.Contains(t, string(body), `#include? "`+override+`"`)
		})
	}
}

// TestLink_missingBaseConfigRefSkipsSilently pins the dangling-ref behavior:
// an XCBuildConfiguration whose baseConfigurationReference id no PBXFileReference
// defines is skipped silently — the pbxproj is malformed in a way we won't repair.
func TestLink_missingBaseConfigRefSkipsSilently(t *testing.T) {
	tmp := t.TempDir()
	pbx := `// !$*UTF8*$!
{
	objects = {
/* Begin PBXFileReference section */
/* End PBXFileReference section */
/* Begin XCBuildConfiguration section */
		BBBBBBBBBBBBBBBBBBBBBBBB /* Debug */ = {
			isa = XCBuildConfiguration;
			baseConfigurationReference = DEADBEEFDEADBEEFDEADBEEF /* Missing.xcconfig */;
			buildSettings = {};
			name = Debug;
		};
/* End XCBuildConfiguration section */
	};
}
`
	projPath := writeProject(t, tmp, "Dangling.xcodeproj", pbx, nil)

	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          projPath,
		OverrideXCConfigPath: override,
	})
	require.NoError(t, err)
	assert.Empty(t, result.ModifiedXCConfigs, "missing-ref branch must not touch any xcconfig")
	assert.Empty(t, result.CreatedSiblings, "missing-ref branch must not create a sibling either")

	pbxAfter, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)
	assert.Equal(t, pbx, string(pbxAfter), "pbxproj must be byte-identical when nothing resolved")
}

// TestLink_workspaceDedupesDuplicateProjectRefs pins the seen-map in
// resolveWorkspace: a workspace listing the same .xcodeproj twice is processed once.
func TestLink_workspaceDedupesDuplicateProjectRefs(t *testing.T) {
	tmp := t.TempDir()

	writeProject(t, tmp, "AppA.xcodeproj", minimalPbxWithBaseRef, map[string]string{
		"Base.xcconfig": "A = 1\n",
	})
	appBPbx := strings.ReplaceAll(minimalPbxWithBaseRef, `path = "Base.xcconfig"`, `path = "BaseB.xcconfig"`)
	appBPbx = strings.ReplaceAll(appBPbx, `/* Base.xcconfig */`, `/* BaseB.xcconfig */`)
	writeProject(t, tmp, "AppB.xcodeproj", appBPbx, map[string]string{
		"BaseB.xcconfig": "B = 1\n",
	})

	wsPath := filepath.Join(tmp, "App.xcworkspace")
	require.NoError(t, os.MkdirAll(wsPath, 0o755))
	contents := `<?xml version="1.0" encoding="UTF-8"?>
<Workspace version = "1.0">
  <FileRef location = "group:AppA.xcodeproj"/>
  <FileRef location = "group:AppB.xcodeproj"/>
  <FileRef location = "group:AppA.xcodeproj"/>
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
	require.Len(t, result.ModifiedXCConfigs, 2)
	assert.Equal(t, 1, strings.Count(strings.Join(result.ModifiedXCConfigs, "\n"), filepath.Join(tmp, "Base.xcconfig")))
}
