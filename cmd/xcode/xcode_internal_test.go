//go:build unit

package xcode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode/invoke"
)

// stubResolveOK replaces resolveXcodeInvocation with a canned spec + observed args.
func stubResolveOK(t *testing.T, spec invoke.InvocationSpec) *struct {
	Calls int
	Cmd   invoke.Command
	Root  string
} {
	t.Helper()
	orig := resolveXcodeInvocation
	t.Cleanup(func() { resolveXcodeInvocation = orig })

	observed := &struct {
		Calls int
		Cmd   invoke.Command
		Root  string
	}{}

	resolveXcodeInvocation = func(_ context.Context, command invoke.Command, repoRoot string) (invoke.InvocationSpec, error) {
		observed.Calls++
		observed.Cmd = command
		observed.Root = repoRoot

		return spec, nil
	}

	return observed
}

// stubWrapper replaces runXcodebuildWrapperFn to capture the argv it would have passed to xcodebuild.
func stubWrapper(t *testing.T) *[]string {
	t.Helper()
	orig := runXcodebuildWrapperFn
	t.Cleanup(func() { runXcodebuildWrapperFn = orig })

	var captured []string
	runXcodebuildWrapperFn = func(_ context.Context, argv []string, _ *cobra.Command) error {
		captured = append([]string(nil), argv...)

		return nil
	}

	return &captured
}

// gitRepoDir sets up a temp dir, makes it a git repo (mkdir .git), chdir there, restores cwd.
func gitRepoDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// t.TempDir may resolve to a path under /private/var (macOS symlink); rely
	// on os.Getwd for the value the code sees post-chdir.
	resolved, err := os.Getwd()
	require.NoError(t, err)

	return resolved
}

func newSubCmdForTest(command invoke.Command) *cobra.Command {
	if command == invoke.CommandTest {
		return newXcodeSubcommand(invoke.CommandTest, "test", "test")
	}

	return newXcodeSubcommand(invoke.CommandBuild, "build", "build")
}

func Test_XcodeSubcommand_BuildsArgvFromResolvedSpec(t *testing.T) {
	gitRepoDir(t)

	stubResolveOK(t, invoke.InvocationSpec{
		Workspace:   "Foo.xcworkspace",
		Scheme:      "Foo",
		Destination: "generic/platform=iOS",
	})
	captured := stubWrapper(t)

	cmd := newSubCmdForTest(invoke.CommandBuild)
	cmd.SetArgs(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	require.NotNil(t, *captured)
	assert.Equal(t, []string{
		"build",
		"-workspace", "Foo.xcworkspace",
		"-scheme", "Foo",
		"-destination", "generic/platform=iOS",
		"CODE_SIGNING_ALLOWED=NO",
		"CODE_SIGN_IDENTITY=",
		"CODE_SIGNING_REQUIRED=NO",
	}, *captured)
}

func Test_XcodeSubcommand_CodesignFlagOptsInto_Codesigning(t *testing.T) {
	gitRepoDir(t)

	stubResolveOK(t, invoke.InvocationSpec{
		Workspace:   "Foo.xcworkspace",
		Scheme:      "Foo",
		Destination: "generic/platform=iOS",
	})
	captured := stubWrapper(t)

	cmd := newSubCmdForTest(invoke.CommandBuild)
	cmd.SetArgs([]string{"--codesign"})

	require.NoError(t, cmd.Execute())

	for _, arg := range *captured {
		assert.NotEqual(t, "CODE_SIGNING_ALLOWED=NO", arg, "codesign flag must suppress the CODE_SIGNING_ALLOWED=NO injection")
	}
}

func Test_XcodeSubcommand_TestSubcommand_PicksTestCommand(t *testing.T) {
	gitRepoDir(t)

	observed := stubResolveOK(t, invoke.InvocationSpec{
		Project:     "Foo.xcodeproj",
		Scheme:      "Foo",
		Destination: "platform=iOS Simulator,name=iPhone 15",
	})
	captured := stubWrapper(t)

	cmd := newSubCmdForTest(invoke.CommandTest)
	cmd.SetArgs(nil)

	require.NoError(t, cmd.Execute())

	assert.Equal(t, invoke.CommandTest, observed.Cmd)
	require.NotEmpty(t, *captured)
	assert.Equal(t, "test", (*captured)[0])
}

func Test_XcodeSubcommand_PositionalArgsPassThrough(t *testing.T) {
	gitRepoDir(t)

	stubResolveOK(t, invoke.InvocationSpec{
		Workspace:   "Foo.xcworkspace",
		Scheme:      "Foo",
		Destination: "generic/platform=iOS",
	})
	captured := stubWrapper(t)

	cmd := newSubCmdForTest(invoke.CommandBuild)
	cmd.SetArgs([]string{"--", "-quiet", "-showBuildTimingSummary"})

	require.NoError(t, cmd.Execute())

	require.NotEmpty(t, *captured)
	// Positional args land after the resolver-driven and codesign args.
	assert.Equal(t, "-quiet", (*captured)[len(*captured)-2])
	assert.Equal(t, "-showBuildTimingSummary", (*captured)[len(*captured)-1])
}

func Test_XcodeSubcommand_Reconfigure_DeletesExistingConfig(t *testing.T) {
	repoRoot := gitRepoDir(t)

	configPath := paths.RepoLocalConfigPath(repoRoot, "xcode-build.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte(`{"scheme":"Old"}`), 0o644))

	stubResolveOK(t, invoke.InvocationSpec{
		Workspace:   "Foo.xcworkspace",
		Scheme:      "New",
		Destination: "generic/platform=iOS",
	})
	stubWrapper(t)

	cmd := newSubCmdForTest(invoke.CommandBuild)
	cmd.SetArgs([]string{"--reconfigure"})

	require.NoError(t, cmd.Execute())

	// The current stubbed resolver does not re-persist, so the file remains absent post-delete.
	_, err := os.Stat(configPath)
	assert.True(t, os.IsNotExist(err), "reconfigure must delete the existing invocation config file")
}

func Test_XcodeSubcommand_Reconfigure_NoOpWhenFileMissing(t *testing.T) {
	gitRepoDir(t)

	stubResolveOK(t, invoke.InvocationSpec{
		Workspace:   "Foo.xcworkspace",
		Scheme:      "Foo",
		Destination: "generic/platform=iOS",
	})
	stubWrapper(t)

	cmd := newSubCmdForTest(invoke.CommandBuild)
	cmd.SetArgs([]string{"--reconfigure"})

	require.NoError(t, cmd.Execute())
}

func Test_XcodeSubcommand_Reconfigure_NoOpOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	observed := stubResolveOK(t, invoke.InvocationSpec{
		Workspace:   "Foo.xcworkspace",
		Scheme:      "Foo",
		Destination: "generic/platform=iOS",
	})
	stubWrapper(t)

	cmd := newSubCmdForTest(invoke.CommandBuild)
	cmd.SetArgs([]string{"--reconfigure"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute(), "reconfigure outside a git repo must not error out")

	assert.Equal(t, 1, observed.Calls, "resolver must still be invoked when reconfigure runs outside a git repo")
	assert.Empty(t, observed.Root, "resolver must be called with empty repoRoot outside a git repo")
}

func Test_XcodeSubcommand_PromptUnavailable_ReturnsUserFacingError(t *testing.T) {
	repoRoot := gitRepoDir(t)

	orig := resolveXcodeInvocation
	t.Cleanup(func() { resolveXcodeInvocation = orig })
	resolveXcodeInvocation = func(_ context.Context, _ invoke.Command, _ string) (invoke.InvocationSpec, error) {
		return invoke.InvocationSpec{}, invoke.ErrPromptUnavailable
	}

	origWrap := runXcodebuildWrapperFn
	t.Cleanup(func() { runXcodebuildWrapperFn = origWrap })
	runXcodebuildWrapperFn = func(_ context.Context, _ []string, _ *cobra.Command) error {
		t.Fatal("wrapper must not be invoked when resolver fails")

		return nil
	}

	cmd := newSubCmdForTest(invoke.CommandBuild)
	cmd.SetArgs(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()

	require.Error(t, err)
	assert.ErrorIs(t, err, invoke.ErrPromptUnavailable)
	assert.Contains(t, err.Error(), paths.RepoLocalConfigPath(repoRoot, "xcode-build.json"), "error message must include the config path so the user can hand-edit")
}

func Test_XcodeSubcommand_NotInGitRepo_ContinuesWithoutRoot(t *testing.T) {
	dir := t.TempDir()

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	observed := stubResolveOK(t, invoke.InvocationSpec{
		Workspace:   "Foo.xcworkspace",
		Scheme:      "Foo",
		Destination: "generic/platform=iOS",
	})
	stubWrapper(t)

	cmd := newSubCmdForTest(invoke.CommandBuild)
	cmd.SetArgs(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	assert.Equal(t, 1, observed.Calls)
	assert.Empty(t, observed.Root, "resolver must be called with empty repoRoot when not in a git repo")
}

func Test_XcodeSubcommand_ResolveError_PropagatesWrapped(t *testing.T) {
	gitRepoDir(t)

	sentinel := errors.New("boom")

	orig := resolveXcodeInvocation
	t.Cleanup(func() { resolveXcodeInvocation = orig })
	resolveXcodeInvocation = func(_ context.Context, _ invoke.Command, _ string) (invoke.InvocationSpec, error) {
		return invoke.InvocationSpec{}, sentinel
	}
	stubWrapper(t)

	cmd := newSubCmdForTest(invoke.CommandBuild)
	cmd.SetArgs(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func Test_XcodeCommand_RegisteredAtTopLevel(t *testing.T) {
	// xcodeCommand is added to common.RootCmd in init(). Assert the tree
	// exposes the new subcommands so `bitrise-build-cache xcode build/test`
	// resolves via cobra.
	child, _, err := xcodeCommand.Find([]string{"build"})
	require.NoError(t, err)
	require.NotNil(t, child)
	assert.Equal(t, "build", child.Use)

	child, _, err = xcodeCommand.Find([]string{"test"})
	require.NoError(t, err)
	require.NotNil(t, child)
	assert.Equal(t, "test", child.Use)
}
