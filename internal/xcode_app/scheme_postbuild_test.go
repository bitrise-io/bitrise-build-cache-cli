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

const schemeNoPostActions = `<?xml version="1.0" encoding="UTF-8"?>
<Scheme
   LastUpgradeVersion = "1500"
   version = "1.3">
   <BuildAction
      parallelizeBuildables = "YES"
      buildImplicitDependencies = "YES">
      <BuildActionEntries>
         <BuildActionEntry
            buildForTesting = "YES"
            buildForRunning = "YES">
            <BuildableReference
               BuildableIdentifier = "primary"
               BlueprintIdentifier = "AAAA">
            </BuildableReference>
         </BuildActionEntry>
      </BuildActionEntries>
   </BuildAction>
   <TestAction
      buildConfiguration = "Debug">
   </TestAction>
</Scheme>
`

const schemeWithOurAction = `<?xml version="1.0" encoding="UTF-8"?>
<Scheme version = "1.3">
   <BuildAction>
      <PostActions>
         <ExecutionAction
            ActionType = "Xcode.IDEStandardExecutionActionsCore.ExecutionActionType.ShellScriptAction"
            ActionID = "io.bitrise.cas.flush">
            <ActionContent
               title = "Bitrise Build Cache: flush + list recent invocations"
               scriptText = "/usr/local/bin/bitrise-build-cache xcelerate flush-session; /usr/local/bin/bitrise-build-cache invocations list --limit 5">
            </ActionContent>
         </ExecutionAction>
      </PostActions>
      <BuildActionEntries/>
   </BuildAction>
</Scheme>
`

const schemeWithForeignPostAction = `<?xml version="1.0" encoding="UTF-8"?>
<Scheme version = "1.3">
   <BuildAction>
      <PostActions>
         <ExecutionAction
            ActionType = "Xcode.IDEStandardExecutionActionsCore.ExecutionActionType.ShellScriptAction"
            ActionID = "com.example.user-script">
            <ActionContent
               title = "User script"
               scriptText = "echo hello">
            </ActionContent>
         </ExecutionAction>
      </PostActions>
      <BuildActionEntries/>
   </BuildAction>
</Scheme>
`

func writeScheme(t *testing.T, dir, name, body string) string {
	t.Helper()
	schemesDir := filepath.Join(dir, "xcshareddata", "xcschemes")
	require.NoError(t, os.MkdirAll(schemesDir, 0o755))
	path := filepath.Join(schemesDir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	return path
}

func TestInjectFlushSessionPostAction_addsBlockWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	schemePath := writeScheme(t, tmp, "App.xcscheme", schemeNoPostActions)

	changed, err := InjectFlushSessionPostAction(utils.DefaultOsProxy{}, schemePath, "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)
	require.True(t, changed)

	body, err := os.ReadFile(schemePath)
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, `ActionID = "io.bitrise.cas.flush"`)
	assert.Contains(t, s, "xcelerate flush-session")
	assert.Contains(t, s, "invocations list --limit 5")
	assert.Contains(t, s, "flush + list recent invocations")
	assert.Contains(t, s, "<PostActions>")
	assert.Contains(t, s, "</PostActions>")
	assert.Contains(t, s, "</BuildAction>", "closing tag preserved")
	assert.Contains(t, s, "<TestAction", "sibling block preserved")
	assert.Equal(t, 1, strings.Count(s, "<PostActions>"))
}

func TestInjectFlushSessionPostAction_idempotentWhenAlreadyPresent(t *testing.T) {
	tmp := t.TempDir()
	schemePath := writeScheme(t, tmp, "App.xcscheme", schemeWithOurAction)
	before, err := os.ReadFile(schemePath)
	require.NoError(t, err)

	changed, err := InjectFlushSessionPostAction(utils.DefaultOsProxy{}, schemePath, "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)
	assert.False(t, changed)

	after, err := os.ReadFile(schemePath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "second inject must be byte-identical")
}

func TestRemoveFlushSessionPostAction_removesAction(t *testing.T) {
	tmp := t.TempDir()
	schemePath := writeScheme(t, tmp, "App.xcscheme", schemeWithOurAction)

	changed, err := RemoveFlushSessionPostAction(utils.DefaultOsProxy{}, schemePath)
	require.NoError(t, err)
	assert.True(t, changed)

	body, err := os.ReadFile(schemePath)
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "io.bitrise.cas.flush")
	assert.NotContains(t, s, "<PostActions>", "empty PostActions must be pruned")
	assert.NotContains(t, s, "</PostActions>")
	assert.Contains(t, s, "<BuildAction>")
	assert.Contains(t, s, "</BuildAction>")
}

func TestInjectFlushSessionPostAction_keepsForeignAction(t *testing.T) {
	tmp := t.TempDir()
	schemePath := writeScheme(t, tmp, "App.xcscheme", schemeWithForeignPostAction)

	changed, err := InjectFlushSessionPostAction(utils.DefaultOsProxy{}, schemePath, "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)
	require.True(t, changed)

	body, err := os.ReadFile(schemePath)
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, `ActionID = "com.example.user-script"`, "foreign action preserved")
	assert.Contains(t, s, "echo hello")
	assert.Contains(t, s, `ActionID = "io.bitrise.cas.flush"`, "our action appended")
	assert.Equal(t, 1, strings.Count(s, "<PostActions>"), "single PostActions block retained")
}

func TestRemoveFlushSessionPostAction_prunesEmptyPostActions(t *testing.T) {
	tmp := t.TempDir()
	schemePath := writeScheme(t, tmp, "App.xcscheme", schemeWithOurAction)

	_, err := RemoveFlushSessionPostAction(utils.DefaultOsProxy{}, schemePath)
	require.NoError(t, err)

	body, err := os.ReadFile(schemePath)
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "<PostActions")
	assert.NotContains(t, s, "</PostActions>")
}

func TestRemoveFlushSessionPostAction_leavesForeignIntact(t *testing.T) {
	tmp := t.TempDir()
	// Scheme has both our action and a foreign one; remove must leave the foreign.
	fixture := strings.Replace(schemeWithOurAction,
		"      </PostActions>",
		`         <ExecutionAction
            ActionType = "Xcode.IDEStandardExecutionActionsCore.ExecutionActionType.ShellScriptAction"
            ActionID = "com.example.user-script">
            <ActionContent
               title = "User script"
               scriptText = "echo hello">
            </ActionContent>
         </ExecutionAction>
      </PostActions>`, 1)
	schemePath := writeScheme(t, tmp, "App.xcscheme", fixture)

	changed, err := RemoveFlushSessionPostAction(utils.DefaultOsProxy{}, schemePath)
	require.NoError(t, err)
	assert.True(t, changed)

	body, err := os.ReadFile(schemePath)
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "io.bitrise.cas.flush")
	assert.Contains(t, s, "com.example.user-script", "foreign action preserved")
	assert.Contains(t, s, "<PostActions>", "block retained since foreign action remains")
}

func TestDiscoverSchemes_findsSharedAndUserSchemes(t *testing.T) {
	tmp := t.TempDir()
	proj := filepath.Join(tmp, "App.xcodeproj")
	require.NoError(t, os.MkdirAll(proj, 0o755))

	writeScheme(t, proj, "App.xcscheme", schemeNoPostActions)
	writeScheme(t, proj, "Tests.xcscheme", schemeNoPostActions)

	userDir := filepath.Join(proj, "xcuserdata", "alice.xcuserdatad", "xcschemes")
	require.NoError(t, os.MkdirAll(userDir, 0o755))
	userScheme := filepath.Join(userDir, "Local.xcscheme")
	require.NoError(t, os.WriteFile(userScheme, []byte(schemeNoPostActions), 0o644))

	// Non-scheme file in schemes dir — must be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(proj, "xcshareddata", "xcschemes", "xcschememanagement.plist"), []byte("plist"), 0o644))

	schemes, err := DiscoverSchemes(utils.DefaultOsProxy{}, proj)
	require.NoError(t, err)
	assert.Len(t, schemes, 3)
}

func TestDiscoverSchemes_missingDirsAreOK(t *testing.T) {
	tmp := t.TempDir()
	proj := filepath.Join(tmp, "Empty.xcodeproj")
	require.NoError(t, os.MkdirAll(proj, 0o755))

	schemes, err := DiscoverSchemes(utils.DefaultOsProxy{}, proj)
	require.NoError(t, err)
	assert.Empty(t, schemes)
}
