//go:build unit

package deriveddata_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

type manifestFixture struct {
	uuid       string
	scheme     string
	signature  string
	title      string
	timeStop   float64
	primaryObs bool
}

func (m manifestFixture) render() string {
	statusBlock := `<key>highLevelStatus</key><string>S</string>`
	if m.primaryObs {
		statusBlock = `<key>primaryObservable</key><dict><key>highLevelStatus</key><string>S</string></dict>`
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logs</key>
	<dict>
		<key>%s</key>
		<dict>
			<key>className</key><string>IDEActivityLogSection</string>
			<key>fileName</key><string>%s.xcactivitylog</string>
			%s
			<key>schemeIdentifier-schemeName</key><string>%s</string>
			<key>signature</key><string>%s</string>
			<key>timeStartedRecording</key><real>%f</real>
			<key>timeStoppedRecording</key><real>%f</real>
			<key>title</key><string>%s</string>
		</dict>
	</dict>
</dict>
</plist>`, m.uuid, m.uuid, statusBlock, m.scheme, m.signature, m.timeStop-10, m.timeStop, m.title)
}

// seed writes the manifest under a fake home with the standard DerivedData layout
// (~/Library/Developer/Xcode/DerivedData/<ddName>/Logs/<subdir>/LogStoreManifest.plist).
// workspacePath is the value written into <DD-root>/info.plist's WorkspacePath.
// Pass an empty workspacePath to skip writing info.plist.
func seed(t *testing.T, home, ddName, subdir string, m manifestFixture, workspacePath string) string {
	t.Helper()
	ddRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData", ddName)
	require.NoError(t, os.MkdirAll(filepath.Join(ddRoot, "Logs", subdir), 0o755))

	manifestPath := filepath.Join(ddRoot, "Logs", subdir, "LogStoreManifest.plist")
	require.NoError(t, os.WriteFile(manifestPath, []byte(m.render()), 0o644))

	if workspacePath != "" {
		info := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>WorkspacePath</key><string>%s</string></dict></plist>`, workspacePath)
		require.NoError(t, os.WriteFile(filepath.Join(ddRoot, "info.plist"), []byte(info), 0o644))
	}

	return manifestPath
}

func TestFinder_PicksLatestStopAcrossRoots(t *testing.T) {
	home := t.TempDir()

	seed(t, home, "App-abc", "Build", manifestFixture{
		uuid:      "OLD",
		scheme:    "OldScheme",
		signature: "Build App project with scheme OldScheme and configuration Debug",
		title:     "Build App project",
		timeStop:  762345688.0,
	}, "/some/path/App.xcworkspace")

	seed(t, home, "Other-xyz", "Build", manifestFixture{
		uuid:      "NEW",
		scheme:    "NewScheme",
		signature: "Building project Other with scheme NewScheme and configuration Release",
		title:     "Building project Other",
		timeStop:  762345988.0,
	}, "/some/path/Other.xcworkspace")

	finder := &deriveddata.Finder{HomeDir: home}
	got, err := finder.LatestForCommand(enrichment.CommandBuild)
	require.NoError(t, err)

	assert.Equal(t, "NewScheme", got.Scheme)
	assert.Equal(t, "Release", got.Configuration)
	assert.Equal(t, "Other.xcworkspace", got.Workspace)
	assert.Empty(t, got.Project)
}

func TestFinder_FiltersByCommand(t *testing.T) {
	home := t.TempDir()

	seed(t, home, "App-abc", "Test", manifestFixture{
		uuid:      "TEST",
		scheme:    "AppTests",
		signature: "Test AppTests",
		title:     "Test AppTests",
		timeStop:  762345988.0,
	}, "")

	finder := &deriveddata.Finder{HomeDir: home}

	_, err := finder.LatestForCommand(enrichment.CommandBuild)
	require.ErrorIs(t, err, deriveddata.ErrNoRecentBuild)

	got, err := finder.LatestForCommand(enrichment.CommandTest)
	require.NoError(t, err)
	assert.Equal(t, "AppTests", got.Scheme)
}

func TestFinder_SkipsZeroStopEntries(t *testing.T) {
	home := t.TempDir()

	// Write a zero-Stop entry directly (bypass timeStop=0 which would still be encoded).
	ddRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-abc")
	require.NoError(t, os.MkdirAll(filepath.Join(ddRoot, "Logs", "Build"), 0o755))

	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logs</key>
	<dict>
		<key>ZERO</key>
		<dict>
			<key>fileName</key><string>ZERO.xcactivitylog</string>
			<key>highLevelStatus</key><string>S</string>
			<key>schemeIdentifier-schemeName</key><string>App</string>
			<key>signature</key><string>Build App project</string>
			<key>timeStartedRecording</key><real>0</real>
			<key>timeStoppedRecording</key><real>0</real>
			<key>title</key><string>Build App project</string>
		</dict>
	</dict>
</dict>
</plist>`
	require.NoError(t, os.WriteFile(filepath.Join(ddRoot, "Logs", "Build", "LogStoreManifest.plist"), []byte(body), 0o644))

	finder := &deriveddata.Finder{HomeDir: home}
	_, err := finder.LatestForCommand(enrichment.CommandBuild)
	require.ErrorIs(t, err, deriveddata.ErrNoRecentBuild)
}

func TestFinder_MissingInfoPlist_LeavesWorkspaceEmpty(t *testing.T) {
	home := t.TempDir()

	seed(t, home, "App-abc", "Build", manifestFixture{
		uuid:      "BUILD",
		scheme:    "App",
		signature: "Build App project with scheme App and configuration Debug",
		title:     "Build App project",
		timeStop:  762345988.0,
	}, "")

	finder := &deriveddata.Finder{HomeDir: home}
	got, err := finder.LatestForCommand(enrichment.CommandBuild)
	require.NoError(t, err)

	assert.Equal(t, "App", got.Scheme)
	assert.Equal(t, "Debug", got.Configuration)
	assert.Empty(t, got.Workspace)
	assert.Empty(t, got.Project)
}

func TestFinder_ProjectFromInfoPlist(t *testing.T) {
	home := t.TempDir()

	seed(t, home, "App-abc", "Build", manifestFixture{
		uuid:      "BUILD",
		scheme:    "App",
		signature: "Build App project with scheme App and configuration Debug",
		title:     "Build App project",
		timeStop:  762345988.0,
	}, "/some/path/App.xcodeproj")

	finder := &deriveddata.Finder{HomeDir: home}
	got, err := finder.LatestForCommand(enrichment.CommandBuild)
	require.NoError(t, err)

	assert.Empty(t, got.Workspace)
	assert.Equal(t, "App.xcodeproj", got.Project)
}

func TestFinder_NoManifests(t *testing.T) {
	home := t.TempDir()

	finder := &deriveddata.Finder{HomeDir: home}
	_, err := finder.LatestForCommand(enrichment.CommandBuild)
	require.ErrorIs(t, err, deriveddata.ErrNoRecentBuild)
}
