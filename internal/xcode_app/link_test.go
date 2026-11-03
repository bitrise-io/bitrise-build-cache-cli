//go:build unit

package xcode_app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
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

	siblingPath := filepath.Join(tmp, paths.XcodeAppSiblingXCConfigFileName)
	assert.Equal(t, siblingPath, result.CreatedSiblings[0])

	body, err := os.ReadFile(siblingPath)
	require.NoError(t, err)
	assert.Contains(t, string(body), LinkBlockStart)
	assert.Contains(t, string(body), `#include? "`+override+`"`)

	pbxBody, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)
	pbxStr := string(pbxBody)
	assert.Contains(t, pbxStr, "baseConfigurationReference = ", "pbxproj must gain a baseConfigurationReference")
	assert.Contains(t, pbxStr, paths.XcodeAppSiblingXCConfigFileName, "pbxproj must reference the sibling by filename")
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
		"baseConfigurationReference = "+cfgs[0].BaseConfigRefID+` /* `+paths.XcodeAppSiblingXCConfigFileName+" */;\n\t\t\t", "", 1)
	require.NotEqual(t, string(pbxAfterFirst), stripped)
	require.NoError(t, os.WriteFile(filepath.Join(projPath, "project.pbxproj"), []byte(stripped), 0o644))

	_, err = Link(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath, OverrideXCConfigPath: override})
	require.NoError(t, err)

	pbxAfterSecond, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(string(pbxAfterSecond), `path = "`+paths.XcodeAppSiblingXCConfigFileName+`"`),
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

	_, statErr := os.Stat(filepath.Join(tmp, paths.XcodeAppSiblingXCConfigFileName))
	assert.True(t, os.IsNotExist(statErr), "sibling xcconfig should be gone")
}

// TestUnlink_scrubsSiblingBaseConfigReference pins the dangling-ref fix: when
// Link() created the sibling, Unlink() must also strip the baseConfigurationReference
// line and the PBXFileReference entry it inserted — otherwise the pbxproj points at
// a deleted object id and Xcode logs it.
func TestUnlink_scrubsSiblingBaseConfigReference(t *testing.T) {
	tmp := t.TempDir()
	projPath := writeProject(t, tmp, "NoBase.xcodeproj", minimalPbxNoBaseRef, nil)

	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath, OverrideXCConfigPath: override})
	require.NoError(t, err)

	pbxAfterLink, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)
	require.Contains(t, string(pbxAfterLink), "baseConfigurationReference = ",
		"precondition: Link() must have inserted a baseConfigurationReference")
	require.Contains(t, string(pbxAfterLink), paths.XcodeAppSiblingXCConfigFileName,
		"precondition: Link() must have inserted a PBXFileReference for the sibling")

	_, err = Unlink(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath})
	require.NoError(t, err)

	pbxAfterUnlink, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)
	assert.NotContains(t, string(pbxAfterUnlink), "baseConfigurationReference",
		"Unlink() must drop the baseConfigurationReference line we inserted")
	assert.NotContains(t, string(pbxAfterUnlink), paths.XcodeAppSiblingXCConfigFileName,
		"Unlink() must drop the sibling PBXFileReference we inserted")
}

// TestUnlink_leavesUserBaseConfigReferenceIntact pins the opposite half: when the
// baseConfigurationReference points at a user-owned xcconfig (not our sibling),
// Unlink() must leave the pbxproj alone — scrubbing would corrupt the project.
func TestUnlink_leavesUserBaseConfigReferenceIntact(t *testing.T) {
	tmp := t.TempDir()
	projPath := writeProject(t, tmp, "Sample.xcodeproj", minimalPbxWithBaseRef, map[string]string{
		"Base.xcconfig": "FOO = BAR\n",
	})
	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	_, err := Link(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath, OverrideXCConfigPath: override})
	require.NoError(t, err)

	pbxBefore, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)

	_, err = Unlink(utils.DefaultOsProxy{}, LinkParams{ProjectPath: projPath})
	require.NoError(t, err)

	pbxAfter, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)
	assert.Equal(t, string(pbxBefore), string(pbxAfter),
		"Unlink() must not edit the pbxproj when baseConfigurationReference points at a user xcconfig")
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

// TestLinkOneProject_ReplacesStaleBaseConfigRef_WhenConfigsChange pins the
// stale-ref recovery: when a previous activate (or hand-edit) left a
// baseConfigurationReference whose PBXFileReference target is gone, Link()
// strips the stale line and attaches a fresh ref to a freshly-minted sibling
// — never leaves the pbxproj with a dangling pointer, never stacks a duplicate.
func TestLinkOneProject_ReplacesStaleBaseConfigRef_WhenConfigsChange(t *testing.T) {
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
	require.Len(t, result.CreatedSiblings, 1, "stale-ref branch must mint a fresh sibling")

	pbxAfter, err := os.ReadFile(filepath.Join(projPath, "project.pbxproj"))
	require.NoError(t, err)
	assert.NotContains(t, string(pbxAfter), "DEADBEEFDEADBEEFDEADBEEF",
		"stale baseConfigurationReference line must be gone")
	assert.Equal(t, 1, strings.Count(string(pbxAfter), "baseConfigurationReference = "),
		"exactly one baseConfigurationReference line, pointing at the fresh sibling")
	assert.Contains(t, string(pbxAfter), paths.XcodeAppSiblingXCConfigFileName,
		"fresh sibling PBXFileReference must be present")
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

// TestResolveXCConfigForConfig_ResolvesPathThroughGroupHierarchy pins Bug 1 — a
// `baseConfigurationReference` fileref nested inside a `PBXGroup` chain with its
// own `path` attribute must resolve to the composed on-disk location, not the
// bare `ref.Path` under projDir.
func TestResolveXCConfigForConfig_ResolvesPathThroughGroupHierarchy(t *testing.T) {
	tmp := t.TempDir()
	// Shared/Configs/Base.xcconfig: fileref inside group "Configs" inside group
	// "Shared"; both groups have `path` attributes and sourceTree=`"<group>"`.
	pbx := `// !$*UTF8*$!
{
	objects = {
/* Begin PBXFileReference section */
		AAAAAAAAAAAAAAAAAAAAAAAA /* Base.xcconfig */ = {isa = PBXFileReference; lastKnownFileType = text.xcconfig; path = "Base.xcconfig"; sourceTree = "<group>"; };
/* End PBXFileReference section */
/* Begin PBXGroup section */
		C0C0C0C0C0C0C0C0C0C0C0C0 /* Configs */ = {
			isa = PBXGroup;
			children = (
				AAAAAAAAAAAAAAAAAAAAAAAA /* Base.xcconfig */,
			);
			path = "Configs";
			sourceTree = "<group>";
		};
		D0D0D0D0D0D0D0D0D0D0D0D0 /* Shared */ = {
			isa = PBXGroup;
			children = (
				C0C0C0C0C0C0C0C0C0C0C0C0 /* Configs */,
			);
			path = "Shared";
			sourceTree = "<group>";
		};
		E0E0E0E0E0E0E0E0E0E0E0E0 /* main */ = {
			isa = PBXGroup;
			children = (
				D0D0D0D0D0D0D0D0D0D0D0D0 /* Shared */,
			);
			sourceTree = "<group>";
		};
/* End PBXGroup section */
/* Begin XCBuildConfiguration section */
		BBBBBBBBBBBBBBBBBBBBBBBB /* Debug */ = {
			isa = XCBuildConfiguration;
			baseConfigurationReference = AAAAAAAAAAAAAAAAAAAAAAAA /* Base.xcconfig */;
			buildSettings = {};
			name = Debug;
		};
/* End XCBuildConfiguration section */
	};
}
`
	projPath := writeProject(t, tmp, "Nested.xcodeproj", pbx, map[string]string{
		"Shared/Configs/Base.xcconfig": "FOO = BAR\n",
	})

	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          projPath,
		OverrideXCConfigPath: override,
	})
	require.NoError(t, err)
	require.Len(t, result.ModifiedXCConfigs, 1,
		"must resolve the nested xcconfig, not fall back to minting a sibling")
	assert.Equal(t, filepath.Join(tmp, "Shared", "Configs", "Base.xcconfig"), result.ModifiedXCConfigs[0],
		"resolved path must prepend both ancestor group paths")

	body, err := os.ReadFile(result.ModifiedXCConfigs[0])
	require.NoError(t, err)
	assert.Contains(t, string(body), `#include? "`+override+`"`)
	assert.Contains(t, string(body), "FOO = BAR", "existing content preserved")
}

// TestResolveXCConfigForConfig_HonorsSOURCE_ROOTSourceTree pins the sourceTree
// branch: when a fileref opts out of the group chain via sourceTree=SOURCE_ROOT,
// the ancestor path chain must be ignored and the ref.Path treated as relative
// to projDir.
func TestResolveXCConfigForConfig_HonorsSOURCE_ROOTSourceTree(t *testing.T) {
	tmp := t.TempDir()
	pbx := `// !$*UTF8*$!
{
	objects = {
/* Begin PBXFileReference section */
		AAAAAAAAAAAAAAAAAAAAAAAA /* Base.xcconfig */ = {isa = PBXFileReference; lastKnownFileType = text.xcconfig; path = "Base.xcconfig"; sourceTree = SOURCE_ROOT; };
/* End PBXFileReference section */
/* Begin PBXGroup section */
		C0C0C0C0C0C0C0C0C0C0C0C0 /* Shared */ = {
			isa = PBXGroup;
			children = (
				AAAAAAAAAAAAAAAAAAAAAAAA /* Base.xcconfig */,
			);
			path = "Shared";
			sourceTree = "<group>";
		};
/* End PBXGroup section */
/* Begin XCBuildConfiguration section */
		BBBBBBBBBBBBBBBBBBBBBBBB /* Debug */ = {
			isa = XCBuildConfiguration;
			baseConfigurationReference = AAAAAAAAAAAAAAAAAAAAAAAA /* Base.xcconfig */;
			buildSettings = {};
			name = Debug;
		};
/* End XCBuildConfiguration section */
	};
}
`
	projPath := writeProject(t, tmp, "SourceRoot.xcodeproj", pbx, map[string]string{
		// SOURCE_ROOT ignores the group chain → file lives at project root.
		"Base.xcconfig": "FOO = BAR\n",
	})

	override := filepath.Join(tmp, "override.xcconfig")
	require.NoError(t, os.WriteFile(override, []byte("// override\n"), 0o644))

	result, err := Link(utils.DefaultOsProxy{}, LinkParams{
		ProjectPath:          projPath,
		OverrideXCConfigPath: override,
	})
	require.NoError(t, err)
	require.Len(t, result.ModifiedXCConfigs, 1)
	assert.Equal(t, filepath.Join(tmp, "Base.xcconfig"), result.ModifiedXCConfigs[0],
		"SOURCE_ROOT sourceTree must bypass the ancestor group path chain")
}

// TestCollectWorkspaceRefs_PrependsGroupLocation pins Bug 2 — a FileRef inside
// a workspace Group with `location="group:SubProject"` must resolve relative
// to the composed `SubProject/<ref>` path, not the bare ref.
func TestCollectWorkspaceRefs_PrependsGroupLocation(t *testing.T) {
	refs := collectWorkspaceRefs(nil, []workspaceGroup{
		{
			Location: "group:SubProject",
			FileRefs: []workspaceFileRef{
				{Location: "container:App.xcodeproj"},
			},
		},
	}, "")
	require.Len(t, refs, 1)
	assert.Equal(t, "container:SubProject/App.xcodeproj", refs[0],
		"group's own location must be prepended to the child FileRef")
}

// TestCollectWorkspaceRefs_AbsoluteGroupReplacesPrefix pins the absolute-group
// branch: a child FileRef inside a group with `location="absolute:/tmp/foo"`
// resolves against the absolute parent path, not the workspace parent dir.
func TestCollectWorkspaceRefs_AbsoluteGroupReplacesPrefix(t *testing.T) {
	refs := collectWorkspaceRefs(nil, []workspaceGroup{
		{
			Location: "absolute:/tmp/foo",
			FileRefs: []workspaceFileRef{
				{Location: "container:App.xcodeproj"},
			},
		},
	}, "")
	require.Len(t, refs, 1)
	assert.Equal(t, "absolute:/tmp/foo/App.xcodeproj", refs[0],
		"absolute parent group must reroot the relative child ref")

	resolved, ok := resolveWorkspaceRef("/does-not-matter/App.xcworkspace", refs[0])
	require.True(t, ok)
	assert.Equal(t, "/tmp/foo/App.xcodeproj", resolved,
		"absolute composed location resolves to the absolute path verbatim")
}

// TestCollectWorkspaceRefs_NestedGroupsCompose pins the recursive composition:
// group within group within workspace must concatenate both group locations
// before the FileRef's own path.
func TestCollectWorkspaceRefs_NestedGroupsCompose(t *testing.T) {
	refs := collectWorkspaceRefs(nil, []workspaceGroup{
		{
			Location: "group:Outer",
			Groups: []workspaceGroup{
				{
					Location: "group:Inner",
					FileRefs: []workspaceFileRef{
						{Location: "container:App.xcodeproj"},
					},
				},
			},
		},
	}, "")
	require.Len(t, refs, 1)
	assert.Equal(t, "container:Outer/Inner/App.xcodeproj", refs[0])
}

// TestCollectWorkspaceRefs_AbsoluteChildOverridesParent pins the opposite
// precedence: a FileRef with `absolute:` ignores any parent-group prefix.
func TestCollectWorkspaceRefs_AbsoluteChildOverridesParent(t *testing.T) {
	refs := collectWorkspaceRefs(nil, []workspaceGroup{
		{
			Location: "group:Shared",
			FileRefs: []workspaceFileRef{
				{Location: "absolute:/elsewhere/App.xcodeproj"},
			},
		},
	}, "")
	require.Len(t, refs, 1)
	assert.Equal(t, "absolute:/elsewhere/App.xcodeproj", refs[0])
}
