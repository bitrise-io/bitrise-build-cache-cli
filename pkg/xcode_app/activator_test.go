//go:build unit

package xcode_app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLogger() log.Logger {
	return log.NewLogger(log.WithOutput(&bytes.Buffer{}))
}

func TestLink_returnsErrUnsupportedPlatformOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin-only assertion")
	}

	a := &Activator{Logger: newLogger()}

	_, err := a.Link(context.Background(), "/does/not/matter.xcodeproj")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedPlatform)
}

func TestUnlink_returnsErrUnsupportedPlatformOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin-only assertion")
	}

	a := &Activator{Logger: newLogger()}

	_, err := a.Unlink(context.Background(), "/does/not/matter.xcodeproj")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedPlatform)
}

func TestLink_happyPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: path resolver reads HOME-rooted xcelerate dir")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	projDir := filepath.Join(home, "Sample.xcodeproj")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	// Build config without a baseConfigurationReference → sibling path exercised.
	pbx := `// !$*UTF8*$!
{
	objects = {
/* Begin PBXFileReference section */
/* End PBXFileReference section */
/* Begin XCBuildConfiguration section */
		CCCCCCCCCCCCCCCCCCCCCCCC /* Debug */ = {
			isa = XCBuildConfiguration;
			buildSettings = {};
			name = Debug;
		};
/* End XCBuildConfiguration section */
	};
}
`
	require.NoError(t, os.WriteFile(filepath.Join(projDir, "project.pbxproj"), []byte(pbx), 0o644))

	a := &Activator{
		Logger: newLogger(),
		Envs:   map[string]string{},
	}

	result, err := a.Link(context.Background(), projDir)
	require.NoError(t, err)
	assert.Empty(t, result.ModifiedXCConfigs)
	require.Len(t, result.CreatedSiblings, 1)
	assert.Equal(t, filepath.Join(home, ".bitrise-build-cache.xcconfig"), result.CreatedSiblings[0])

	body, err := os.ReadFile(result.CreatedSiblings[0])
	require.NoError(t, err)
	expectedOverride := filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	assert.Contains(t, string(body), `#include? "`+expectedOverride+`"`)
}

func TestUnlink_happyPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: path resolver reads HOME-rooted xcelerate dir")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	projDir := filepath.Join(home, "Sample.xcodeproj")
	require.NoError(t, os.MkdirAll(projDir, 0o755))

	// Simulate a prior Link run: existing base config with marker block.
	baseXCConfigAbs := filepath.Join(home, "Base.xcconfig")
	pbx := `// !$*UTF8*$!
{
	objects = {
/* Begin PBXFileReference section */
		AAAAAAAAAAAAAAAAAAAAAAAA /* Base.xcconfig */ = {isa = PBXFileReference; lastKnownFileType = text.xcconfig; path = "Base.xcconfig"; sourceTree = "<group>"; };
/* End PBXFileReference section */
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
	require.NoError(t, os.WriteFile(filepath.Join(projDir, "project.pbxproj"), []byte(pbx), 0o644))
	require.NoError(t, os.WriteFile(baseXCConfigAbs, []byte(
		"FOO = BAR\n"+
			"// [start] bitrise-build-cache xcode link\n"+
			`#include? "/tmp/override.xcconfig"`+"\n"+
			"// [end] bitrise-build-cache xcode link\n"), 0o644))

	a := &Activator{
		Logger: newLogger(),
		Envs:   map[string]string{},
	}

	result, err := a.Unlink(context.Background(), projDir)
	require.NoError(t, err)
	require.Len(t, result.ModifiedXCConfigs, 1)

	body, err := os.ReadFile(baseXCConfigAbs)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "[start] bitrise-build-cache xcode link")
	assert.Contains(t, string(body), "FOO = BAR")
}
