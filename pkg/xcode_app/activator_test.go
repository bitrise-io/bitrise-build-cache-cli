//go:build unit

package xcode_app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xa "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app"
)

func newLogger() log.Logger {
	return log.NewLogger(log.WithOutput(&bytes.Buffer{}))
}

func TestEnable_returnsErrUnsupportedPlatformOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin-only assertion")
	}

	a := &Activator{Logger: newLogger()}

	_, err := a.Enable(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedPlatform)
}

func TestDisable_returnsErrUnsupportedPlatformOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin-only assertion")
	}

	a := &Activator{Logger: newLogger()}

	_, err := a.Disable(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedPlatform)
}

func TestEnable_missingXcelerateConfigReturnsSentinel(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: this path runs after the platform check")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoDirExists(t, filepath.Join(home, ".bitrise-xcelerate"))

	a := &Activator{Logger: newLogger(), Envs: map[string]string{}}

	_, err := a.Enable(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrXcelerateNotConfigured)
}

func TestEnable_corruptXcelerateConfigIsNotSentinel(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: this path runs after the platform check")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeXcelerateConfig(t, home, "not json {")

	a := &Activator{Logger: newLogger(), Envs: map[string]string{}}

	_, err := a.Enable(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrXcelerateNotConfigured,
		"corrupt config should surface decode error, not point user at `activate xcode`")
	assert.Contains(t, err.Error(), "read xcelerate config")
}

func TestEnable_emptyProxySocketIsError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: this path runs after the platform check")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeXcelerateConfig(t, home, `{"proxySocketPath":""}`)

	a := &Activator{Logger: newLogger(), Envs: map[string]string{}}

	_, err := a.Enable(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty proxy socket path")
}

func TestEnable_happyPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only orchestration (uses ~/Library/LaunchAgents path shape and HOME)")
	}

	ownOverride := func(home string) string {
		return filepath.Join(home, ".bitrise-xcelerate", "xcode-app.xcconfig")
	}

	tests := []struct {
		name                 string
		previousXCConfig     string
		runningPIDs          []int
		runningErr           error
		wantPreviousChained  bool
		wantRunningReflected []int
	}{
		{
			name:                 "no prior override, Xcode not running",
			previousXCConfig:     "",
			wantPreviousChained:  false,
			wantRunningReflected: nil,
		},
		{
			name:                 "prior override is ours — do not chain (would loop)",
			previousXCConfig:     "SELF",
			wantPreviousChained:  false,
			wantRunningReflected: nil,
		},
		{
			name:                 "prior override is user's — chain it",
			previousXCConfig:     "/Users/me/custom.xcconfig",
			wantPreviousChained:  true,
			wantRunningReflected: nil,
		},
		{
			name:                 "Xcode running — PIDs surfaced for CLI warn",
			previousXCConfig:     "",
			runningPIDs:          []int{4242, 4243},
			wantRunningReflected: []int{4242, 4243},
		},
		{
			name:                 "pgrep error is swallowed, PIDs stay nil",
			previousXCConfig:     "",
			runningErr:           errors.New("pgrep boom"),
			wantRunningReflected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			writeXcelerateConfig(t, home, `{"proxySocketPath":"/tmp/xcelerate-proxy.sock"}`)

			priorValue := tc.previousXCConfig
			if priorValue == "SELF" {
				priorValue = ownOverride(home)
			}

			runner := &scriptedLaunchctlRunner{getenvValue: priorValue}
			checker := &fakeXcodeChecker{pids: tc.runningPIDs, err: tc.runningErr}

			a := &Activator{
				Logger:       newLogger(),
				Envs:         map[string]string{},
				Launchctl:    xa.LaunchctlClient{Runner: runner, Bin: "/fake/launchctl"},
				XcodeChecker: checker,
			}

			result, err := a.Enable(context.Background())
			require.NoError(t, err)

			assert.Equal(t, ownOverride(home), result.XCConfigPath)
			assert.Equal(t, "/tmp/xcelerate-proxy.sock", result.XcelerateProxySocket)
			assert.Equal(t, tc.wantRunningReflected, result.RunningXcodePIDs)

			body, readErr := os.ReadFile(result.XCConfigPath) //nolint:gosec // test-controlled path
			require.NoError(t, readErr)
			bodyStr := string(body)
			assert.Contains(t, bodyStr, "COMPILATION_CACHE_REMOTE_SERVICE_PATH = /tmp/xcelerate-proxy.sock")

			if tc.wantPreviousChained {
				assert.Equal(t, priorValue, result.PreviousXCConfigPath)
				assert.Contains(t, bodyStr, "#include? \""+priorValue+"\"")
			} else {
				assert.Empty(t, result.PreviousXCConfigPath)
				assert.NotContains(t, bodyStr, "#include?")
			}

			plistBody, plistErr := os.ReadFile(result.LaunchAgentPlistPath) //nolint:gosec // test-controlled path
			require.NoError(t, plistErr)
			assert.Contains(t, string(plistBody), "<string>"+result.XCConfigPath+"</string>")

			assert.True(t, runner.calledSetenv, "launchctl setenv was not invoked")
			assert.True(t, runner.calledBootstrap, "launchctl bootstrap was not invoked")
		})
	}
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
		Logger:       newLogger(),
		Envs:         map[string]string{},
		XcodeChecker: &fakeXcodeChecker{},
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
	// Project path references xcconfig as "Base.xcconfig" relative to project dir;
	// resolved against filepath.Dir(projectPath) = home. Place file at home/Base.xcconfig.
	require.NoError(t, os.WriteFile(filepath.Join(projDir, "project.pbxproj"), []byte(pbx), 0o644))
	require.NoError(t, os.WriteFile(baseXCConfigAbs, []byte(
		"FOO = BAR\n"+
			"// [start] bitrise-build-cache xcode-app link\n"+
			`#include? "/tmp/override.xcconfig"`+"\n"+
			"// [end] bitrise-build-cache xcode-app link\n"), 0o644))

	a := &Activator{
		Logger:       newLogger(),
		Envs:         map[string]string{},
		XcodeChecker: &fakeXcodeChecker{},
	}

	result, err := a.Unlink(context.Background(), projDir)
	require.NoError(t, err)
	require.Len(t, result.ModifiedXCConfigs, 1)

	body, err := os.ReadFile(baseXCConfigAbs)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "[start] bitrise-build-cache xcode-app link")
	assert.Contains(t, string(body), "FOO = BAR")
}

func writeXcelerateConfig(t *testing.T, home, body string) {
	t.Helper()

	dir := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644))
}

// scriptedLaunchctlRunner is a fake exec.Runner that recognises the launchctl
// subcommands the Activator issues (getenv/setenv/bootout/bootstrap/unsetenv)
// and returns a canned value for getenv.
type scriptedLaunchctlRunner struct {
	getenvValue     string
	calledSetenv    bool
	calledBootstrap bool
	calledBootout   bool
	calledUnsetenv  bool
}

func (r *scriptedLaunchctlRunner) Run(_ context.Context, _ string, args ...string) (string, string, int, error) {
	if len(args) == 0 {
		return "", "", 0, nil
	}

	switch args[0] {
	case "getenv":
		if r.getenvValue == "" {
			return "", "", 113, nil
		}

		return r.getenvValue + "\n", "", 0, nil
	case "setenv":
		r.calledSetenv = true
	case "bootstrap":
		r.calledBootstrap = true
	case "bootout":
		r.calledBootout = true
	case "unsetenv":
		r.calledUnsetenv = true
	}

	return "", "", 0, nil
}

type fakeXcodeChecker struct {
	pids []int
	err  error
}

func (f *fakeXcodeChecker) RunningPIDs(_ context.Context) ([]int, error) {
	return f.pids, f.err
}
