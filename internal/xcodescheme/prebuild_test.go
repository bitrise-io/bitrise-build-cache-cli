//go:build unit

package xcodescheme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleScheme = `<?xml version="1.0" encoding="UTF-8"?>
<Scheme
   LastUpgradeVersion = "1600"
   version = "2.0">
   <BuildAction
      parallelizeBuildables = "YES"
      buildImplicitDependencies = "YES">
      <BuildActionEntries>
         <BuildActionEntry
            buildForTesting = "YES"
            buildForRunning = "YES">
            <BuildableReference
               BuildableIdentifier = "primary"
               BuildableName = "App.app"
               BlueprintName = "App"
               ReferencedContainer = "container:App.xcodeproj">
            </BuildableReference>
         </BuildActionEntry>
      </BuildActionEntries>
   </BuildAction>
   <TestAction
      buildConfiguration = "Debug">
   </TestAction>
</Scheme>
`

func writeTempScheme(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "App.xcscheme")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func TestInstall_addsPreActionToFreshScheme(t *testing.T) {
	path := writeTempScheme(t, sampleScheme)

	status, err := Install(path)
	require.NoError(t, err)
	assert.Equal(t, StatusInstalled, status)

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)

	assert.Contains(t, got, "<PreActions>", "PreActions block should be injected")
	assert.Contains(t, got, Marker, "marker should be present in script body")
	assert.Contains(t, got, "bitrise-build-cache doctor --no-update-check", "doctor invocation should be in script")
	assert.Contains(t, got, "<BuildActionEntries>", "original BuildActionEntries should still be there")

	preIdx := strings.Index(got, "<PreActions>")
	entriesIdx := strings.Index(got, "<BuildActionEntries>")
	assert.Less(t, preIdx, entriesIdx, "<PreActions> must precede <BuildActionEntries>")
}

func TestInstall_isIdempotent(t *testing.T) {
	path := writeTempScheme(t, sampleScheme)

	_, err := Install(path)
	require.NoError(t, err)

	first, err := os.ReadFile(path)
	require.NoError(t, err)

	status, err := Install(path)
	require.NoError(t, err)
	assert.Equal(t, StatusAlreadyInstalled, status)

	second, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, first, second, "second install should not modify the file")
}

func TestInstall_errorsOnMissingBuildAction(t *testing.T) {
	path := writeTempScheme(t, `<?xml version="1.0"?><Scheme></Scheme>`)

	_, err := Install(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BuildAction")
}

func TestUninstall_removesInjectedBlock(t *testing.T) {
	path := writeTempScheme(t, sampleScheme)

	_, err := Install(path)
	require.NoError(t, err)

	status, err := Uninstall(path)
	require.NoError(t, err)
	assert.Equal(t, StatusUninstalled, status)

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)

	assert.NotContains(t, got, Marker)
	assert.NotContains(t, got, "<PreActions>")
	assert.Contains(t, got, "<BuildActionEntries>", "user content untouched")
}

func TestUninstall_isIdempotentWhenNotInstalled(t *testing.T) {
	path := writeTempScheme(t, sampleScheme)

	status, err := Uninstall(path)
	require.NoError(t, err)
	assert.Equal(t, StatusNotInstalled, status)
}

func TestUninstall_preservesUnrelatedPreActions(t *testing.T) {
	customerScheme := strings.Replace(sampleScheme,
		`      <BuildActionEntries>`,
		`      <PreActions>
         <ExecutionAction
            ActionType = "Xcode.IDEStandardExecutionActionsCore.ExecutionActionType.ShellScriptAction">
            <ActionContent
               title = "Customer's own script"
               scriptText = "echo hello">
            </ActionContent>
         </ExecutionAction>
      </PreActions>
      <BuildActionEntries>`,
		1,
	)
	path := writeTempScheme(t, customerScheme)

	_, err := Install(path)
	require.NoError(t, err)

	mid, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(mid), "echo hello", "customer's PreActions must survive install")
	assert.Contains(t, string(mid), Marker, "our marker must be present")

	_, err = Uninstall(path)
	require.NoError(t, err)

	final, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(final), "echo hello", "customer's PreActions must survive uninstall")
	assert.NotContains(t, string(final), Marker, "our marker must be gone")
}

func TestResolveSchemePath_found(t *testing.T) {
	dir := t.TempDir()
	schemes := filepath.Join(dir, "App.xcodeproj", "xcshareddata", "xcschemes")
	require.NoError(t, os.MkdirAll(schemes, 0o755))
	wanted := filepath.Join(schemes, "App.xcscheme")
	require.NoError(t, os.WriteFile(wanted, []byte("<?xml?>"), 0o600))

	got, err := ResolveSchemePath(filepath.Join(dir, "App.xcodeproj"), "App")
	require.NoError(t, err)
	assert.Equal(t, wanted, got)
}

func TestResolveSchemePath_notFound(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveSchemePath(filepath.Join(dir, "App.xcodeproj"), "Missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Missing")
}
