//go:build unit

package invoke_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	utilsmocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode/invoke"
)

func writeConfig(t *testing.T, repoRoot string, cmd invoke.Command, spec invoke.InvocationSpec) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".bitrise-build-cache")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	data, err := json.MarshalIndent(spec, "", "  ")
	require.NoError(t, err)

	name := "xcode-build.json"
	if cmd == invoke.CommandTest {
		name = "xcode-test.json"
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o644))
}

func readConfig(t *testing.T, repoRoot string, cmd invoke.Command) invoke.InvocationSpec {
	t.Helper()
	name := "xcode-build.json"
	if cmd == invoke.CommandTest {
		name = "xcode-test.json"
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".bitrise-build-cache", name))
	require.NoError(t, err)

	var spec invoke.InvocationSpec
	require.NoError(t, json.Unmarshal(data, &spec))

	return spec
}

// seedFinderHome writes a manifest under a fake home so a *deriveddata.Finder
// pointed at that home returns the requested LatestBuild.
func seedFinderHome(t *testing.T, result deriveddata.LatestBuild) string {
	t.Helper()

	home := t.TempDir()
	ddRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-abc")
	require.NoError(t, os.MkdirAll(filepath.Join(ddRoot, "Logs", "Build"), 0o755))

	sig := "Build App project"
	if result.Configuration != "" {
		sig = "Build App project with scheme " + result.Scheme + " and configuration " + result.Configuration
	}

	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logs</key>
	<dict>
		<key>BUILD</key>
		<dict>
			<key>fileName</key><string>BUILD.xcactivitylog</string>
			<key>highLevelStatus</key><string>S</string>
			<key>schemeIdentifier-schemeName</key><string>` + result.Scheme + `</string>
			<key>signature</key><string>` + sig + `</string>
			<key>timeStartedRecording</key><real>762345678.0</real>
			<key>timeStoppedRecording</key><real>762345988.0</real>
			<key>title</key><string>` + sig + `</string>
		</dict>
	</dict>
</dict>
</plist>`
	require.NoError(t, os.WriteFile(filepath.Join(ddRoot, "Logs", "Build", "LogStoreManifest.plist"), []byte(body), 0o644))

	container := result.Workspace
	if container == "" {
		container = result.Project
	}
	if container != "" {
		info := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>WorkspacePath</key><string>/x/` + container + `</string></dict></plist>`
		require.NoError(t, os.WriteFile(filepath.Join(ddRoot, "info.plist"), []byte(info), 0o644))
	}

	return home
}

func TestResolve_FullConfig_NoDiscoveryNoPrompt(t *testing.T) {
	repoRoot := t.TempDir()
	writeConfig(t, repoRoot, invoke.CommandBuild, invoke.InvocationSpec{
		Workspace:     "App.xcworkspace",
		Scheme:        "App",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
	})

	promptCalled := false
	r := &invoke.Resolver{
		Prompt: &PromptMock{FillFunc: func(_ context.Context, _ *invoke.InvocationSpec) error {
			promptCalled = true

			return nil
		}},
		Finder: &deriveddata.Finder{HomeDir: t.TempDir()},
	}

	got, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)
	assert.False(t, promptCalled)
	assert.Equal(t, "App.xcworkspace", got.Workspace)
	assert.Equal(t, "App", got.Scheme)
	assert.Equal(t, "Debug", got.Configuration)
	assert.Equal(t, "generic/platform=iOS", got.Destination)
}

func TestResolve_PartialConfig_MergesFinderThenPrompt(t *testing.T) {
	repoRoot := t.TempDir()
	writeConfig(t, repoRoot, invoke.CommandBuild, invoke.InvocationSpec{
		Configuration: "Release",
		ExtraArgs:     []string{"-quiet"},
	})

	home := seedFinderHome(t, deriveddata.LatestBuild{
		Workspace:     "App.xcworkspace",
		Scheme:        "App",
		Configuration: "Debug",
	})

	prompt := &PromptMock{FillFunc: func(_ context.Context, spec *invoke.InvocationSpec) error {
		assert.Equal(t, "App.xcworkspace", spec.Workspace)
		assert.Equal(t, "App", spec.Scheme)
		assert.Equal(t, "Release", spec.Configuration, "config's Configuration must not be overwritten by discovery")
		assert.Empty(t, spec.Destination)

		spec.Destination = "generic/platform=iOS"

		return nil
	}}

	r := &invoke.Resolver{
		Prompt: prompt,
		Finder: &deriveddata.Finder{HomeDir: home},
	}

	got, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)
	assert.Equal(t, 1, len(prompt.FillCalls()))
	assert.Equal(t, "App.xcworkspace", got.Workspace)
	assert.Equal(t, "App", got.Scheme)
	assert.Equal(t, "Release", got.Configuration)
	assert.Equal(t, "generic/platform=iOS", got.Destination)
	assert.Equal(t, []string{"-quiet"}, got.ExtraArgs, "ExtraArgs from existing config must be preserved")

	persisted := readConfig(t, repoRoot, invoke.CommandBuild)
	assert.Equal(t, got, persisted)
}

func TestResolve_NoConfig_FinderFull_DestinationStillPrompted(t *testing.T) {
	repoRoot := t.TempDir()

	home := seedFinderHome(t, deriveddata.LatestBuild{
		Workspace:     "App.xcworkspace",
		Scheme:        "App",
		Configuration: "Debug",
	})

	prompt := &PromptMock{FillFunc: func(_ context.Context, spec *invoke.InvocationSpec) error {
		spec.Destination = "generic/platform=iOS"

		return nil
	}}

	r := &invoke.Resolver{
		Prompt: prompt,
		Finder: &deriveddata.Finder{HomeDir: home},
	}

	got, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)
	assert.Equal(t, "App.xcworkspace", got.Workspace)
	assert.Equal(t, "App", got.Scheme)
	assert.Equal(t, "Debug", got.Configuration)
	assert.Equal(t, "generic/platform=iOS", got.Destination)
}

func TestResolve_NoRecentBuild_PromptFillsEverything(t *testing.T) {
	repoRoot := t.TempDir()

	prompt := &PromptMock{FillFunc: func(_ context.Context, spec *invoke.InvocationSpec) error {
		spec.Workspace = "App.xcworkspace"
		spec.Scheme = "App"
		spec.Destination = "generic/platform=iOS"

		return nil
	}}

	r := &invoke.Resolver{
		Prompt: prompt,
		Finder: &deriveddata.Finder{HomeDir: t.TempDir()},
	}

	got, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)
	assert.Equal(t, "App.xcworkspace", got.Workspace)
	assert.Equal(t, "App", got.Scheme)
	assert.Equal(t, "generic/platform=iOS", got.Destination)
}

func TestResolve_PromptReturnsErrPromptUnavailable(t *testing.T) {
	repoRoot := t.TempDir()

	prompt := &PromptMock{FillFunc: func(_ context.Context, _ *invoke.InvocationSpec) error {
		return invoke.ErrPromptUnavailable
	}}

	r := &invoke.Resolver{
		Prompt: prompt,
		Finder: &deriveddata.Finder{HomeDir: t.TempDir()},
	}

	_, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.ErrorIs(t, err, invoke.ErrPromptUnavailable)
}

func TestResolve_TolerateUnknownJSONFields(t *testing.T) {
	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, ".bitrise-build-cache")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "xcode-build.json"), []byte(`{
	"workspace": "App.xcworkspace",
	"scheme": "App",
	"destination": "generic/platform=iOS",
	"sdk": "iphonesimulator"
}`), 0o644))

	r := &invoke.Resolver{
		Finder: &deriveddata.Finder{HomeDir: t.TempDir()},
	}

	got, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)
	assert.Equal(t, "App.xcworkspace", got.Workspace)
}

func TestResolve_UnknownJSONFieldsDroppedOnPersist(t *testing.T) {
	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, ".bitrise-build-cache")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "xcode-build.json"), []byte(`{
	"workspace": "App.xcworkspace",
	"scheme": "App",
	"destination": "generic/platform=iOS",
	"sdk": "iphonesimulator"
}`), 0o644))

	r := &invoke.Resolver{
		Finder: &deriveddata.Finder{HomeDir: t.TempDir()},
	}

	_, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "xcode-build.json"))
	require.NoError(t, err)

	var asMap map[string]any
	require.NoError(t, json.Unmarshal(raw, &asMap))

	_, sdkPresent := asMap["sdk"]
	assert.False(t, sdkPresent, "unknown field 'sdk' must be dropped on persist")
	assert.Equal(t, "App.xcworkspace", asMap["workspace"])
	assert.Equal(t, "App", asMap["scheme"])
	assert.Equal(t, "generic/platform=iOS", asMap["destination"])
}

func TestResolve_WorkspaceWinsOverProject(t *testing.T) {
	repoRoot := t.TempDir()
	writeConfig(t, repoRoot, invoke.CommandBuild, invoke.InvocationSpec{
		Workspace:   "App.xcworkspace",
		Project:     "App.xcodeproj",
		Scheme:      "App",
		Destination: "generic/platform=iOS",
	})

	r := &invoke.Resolver{
		Finder: &deriveddata.Finder{HomeDir: t.TempDir()},
	}

	got, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)
	assert.Equal(t, "App.xcworkspace", got.Workspace)
	assert.Empty(t, got.Project, "workspace wins when both are set")

	persisted := readConfig(t, repoRoot, invoke.CommandBuild)
	assert.Equal(t, "App.xcworkspace", persisted.Workspace)
	assert.Empty(t, persisted.Project)
}

func TestResolve_PersistsUnderRepoLocalDir(t *testing.T) {
	repoRoot := t.TempDir()

	prompt := &PromptMock{FillFunc: func(_ context.Context, spec *invoke.InvocationSpec) error {
		spec.Workspace = "App.xcworkspace"
		spec.Scheme = "App"
		spec.Destination = "generic/platform=iOS"

		return nil
	}}

	r := &invoke.Resolver{
		Prompt: prompt,
		Finder: &deriveddata.Finder{HomeDir: t.TempDir()},
	}

	_, err := r.Resolve(context.Background(), invoke.CommandTest, repoRoot)
	require.NoError(t, err)

	persisted := filepath.Join(repoRoot, ".bitrise-build-cache", "xcode-test.json")
	_, err = os.Stat(persisted)
	require.NoError(t, err)
}

func TestResolve_EmptyRepoRoot_NoPersist(t *testing.T) {
	prompt := &PromptMock{FillFunc: func(_ context.Context, spec *invoke.InvocationSpec) error {
		spec.Workspace = "App.xcworkspace"
		spec.Scheme = "App"
		spec.Destination = "generic/platform=iOS"

		return nil
	}}

	r := &invoke.Resolver{
		Prompt: prompt,
		Finder: &deriveddata.Finder{HomeDir: t.TempDir()},
	}

	_, err := r.Resolve(context.Background(), invoke.CommandBuild, "")
	require.NoError(t, err)
}

func TestResolve_ProjectHintFromJSONBypassesCwdScan(t *testing.T) {
	repoRoot := t.TempDir()
	writeConfig(t, repoRoot, invoke.CommandBuild, invoke.InvocationSpec{
		Workspace: "Foo.xcworkspace",
	})

	prompt := &PromptMock{FillFunc: func(_ context.Context, spec *invoke.InvocationSpec) error {
		spec.Scheme = "Foo"
		spec.Destination = "generic/platform=iOS"

		return nil
	}}

	finder := &deriveddata.Finder{HomeDir: t.TempDir()}
	r := &invoke.Resolver{
		Prompt: prompt,
		Finder: finder,
		Cwd:    t.TempDir(), // ensure Cwd would not resolve to repoRoot
	}

	_, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(repoRoot, "Foo.xcworkspace"), finder.ProjectPathHint)
}

func TestResolve_ProjectHintFromCwdScan(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "App.xcworkspace"), 0o755))

	prompt := &PromptMock{FillFunc: func(_ context.Context, spec *invoke.InvocationSpec) error {
		spec.Workspace = "App.xcworkspace"
		spec.Scheme = "App"
		spec.Destination = "generic/platform=iOS"

		return nil
	}}

	finder := &deriveddata.Finder{HomeDir: t.TempDir()}
	r := &invoke.Resolver{
		Prompt: prompt,
		Finder: finder,
		Cwd:    repoRoot,
	}

	_, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(repoRoot, "App.xcworkspace"), finder.ProjectPathHint)
}

func TestResolve_NoProjectHintFallsBackToGlobal(t *testing.T) {
	repoRoot := t.TempDir()

	prompt := &PromptMock{FillFunc: func(_ context.Context, spec *invoke.InvocationSpec) error {
		spec.Workspace = "App.xcworkspace"
		spec.Scheme = "App"
		spec.Destination = "generic/platform=iOS"

		return nil
	}}

	finder := &deriveddata.Finder{HomeDir: t.TempDir()}
	r := &invoke.Resolver{
		Prompt: prompt,
		Finder: finder,
		Cwd:    repoRoot,
	}

	_, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.NoError(t, err)
	assert.Empty(t, finder.ProjectPathHint)
}

func TestResolve_CwdFallback_GetwdErrorSurfaces(t *testing.T) {
	repoRoot := t.TempDir()

	getwdErr := errors.New("boom: getwd failed")
	proxy := &utilsmocks.OsProxyMock{
		GetwdFunc: func() (string, error) {
			return "", getwdErr
		},
		ReadFileIfExistsFunc: func(string) (string, bool, error) {
			return "", false, nil
		},
	}

	prompt := &PromptMock{FillFunc: func(_ context.Context, _ *invoke.InvocationSpec) error {
		t.Fatal("prompt must not be called when cwd resolution fails")

		return nil
	}}

	r := &invoke.Resolver{
		Prompt:  prompt,
		Finder:  &deriveddata.Finder{HomeDir: t.TempDir()},
		OsProxy: proxy,
	}

	_, err := r.Resolve(context.Background(), invoke.CommandBuild, repoRoot)
	require.Error(t, err)
	assert.ErrorIs(t, err, getwdErr, "Getwd failure must surface — broken environment, not a missing hint")
}

func TestInvocationSpec_JSONRoundTrip(t *testing.T) {
	original := invoke.InvocationSpec{
		Workspace:     "App.xcworkspace",
		Scheme:        "App",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
		ExtraArgs:     []string{"-quiet", "OTHER_LDFLAGS=-lfoo"},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded invoke.InvocationSpec
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, original, decoded)
}

func TestBuildArgv(t *testing.T) {
	tests := []struct {
		name     string
		spec     invoke.InvocationSpec
		command  invoke.Command
		codesign bool
		want     []string
	}{
		{
			name: "build with workspace and no codesign injects skip flags",
			spec: invoke.InvocationSpec{
				Workspace:   "App.xcworkspace",
				Scheme:      "App",
				Destination: "generic/platform=iOS",
			},
			command: invoke.CommandBuild,
			want: []string{
				"build",
				"-workspace", "App.xcworkspace",
				"-scheme", "App",
				"-destination", "generic/platform=iOS",
				"CODE_SIGNING_ALLOWED=NO",
				"CODE_SIGN_IDENTITY=",
				"CODE_SIGNING_REQUIRED=NO",
			},
		},
		{
			name: "test with project + configuration + extra args",
			spec: invoke.InvocationSpec{
				Project:       "App.xcodeproj",
				Scheme:        "AppTests",
				Configuration: "Debug",
				Destination:   "platform=iOS Simulator,name=iPhone 15",
				ExtraArgs:     []string{"-quiet"},
			},
			command: invoke.CommandTest,
			want: []string{
				"test",
				"-project", "App.xcodeproj",
				"-scheme", "AppTests",
				"-configuration", "Debug",
				"-destination", "platform=iOS Simulator,name=iPhone 15",
				"CODE_SIGNING_ALLOWED=NO",
				"CODE_SIGN_IDENTITY=",
				"CODE_SIGNING_REQUIRED=NO",
				"-quiet",
			},
		},
		{
			name: "codesign=true skips signing env injections",
			spec: invoke.InvocationSpec{
				Workspace:   "App.xcworkspace",
				Scheme:      "App",
				Destination: "generic/platform=iOS",
			},
			command:  invoke.CommandBuild,
			codesign: true,
			want: []string{
				"build",
				"-workspace", "App.xcworkspace",
				"-scheme", "App",
				"-destination", "generic/platform=iOS",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := invoke.BuildArgv(tt.spec, tt.command, tt.codesign)
			assert.Equal(t, tt.want, got)
		})
	}
}
