//go:build unit

package xcelerate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	logmocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func liteTestLogger() log.Logger {
	l := &logmocks.Logger{}
	for _, m := range []string{"Infof", "Debugf", "Warnf", "Errorf", "TInfof", "TDebugf", "TDonef"} {
		l.On(m).Return()
		l.On(m, mock.Anything).Return()
		l.On(m, mock.Anything, mock.Anything).Return()
		l.On(m, mock.Anything, mock.Anything, mock.Anything).Return()
	}

	return l
}

// isolate keeps the resolver away from anything real: HOME alone is not enough,
// because the stores also read the OS keychain.
func isolate(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)

	return home
}

// populatedCIEnv is everything an external CI injects into a build. Lite has to
// exclude all of it, and the exclusion must not depend on the environment
// happening to be empty — a warmup that runs inside a build sees exactly this.
func populatedCIEnv() map[string]string {
	return map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_REPOSITORY": "acme/app-repo-abcdef12",
		"GITHUB_RUN_ID":     "run-id-abcdef12",
		"GITHUB_JOB":        "job-name-abcdef12",
		auth.EnvAuthToken:   "auth-token-abcdef12",
		auth.EnvWorkspaceID: "workspace-id-abcdef12",
	}
}

func whichCommandFunc(t *testing.T) utils.CommandFunc {
	t.Helper()

	return func(_ context.Context, _ string, _ ...string) utils.Command {
		return &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) { return []byte("/usr/bin/xcodebuild"), nil },
		}
	}
}

// The exclusion has to be structural. Feeding an empty environment proves
// nothing: such a test would pass with Lite ignored entirely.
func TestNewConfig_LiteExcludesAFullyPopulatedCIEnvironment(t *testing.T) {
	isolate(t)
	envs := populatedCIEnv()

	osProxyMock := &utilsMocks.OsProxyMock{
		TempDirFunc:  func() string { return t.TempDir() },
		HostnameFunc: func() (string, error) { return "test-host", nil },
	}

	config, err := NewConfig(t.Context(), liteTestLogger(), Params{BuildCacheEnabled: true, Lite: true},
		envs, osProxyMock, whichCommandFunc(t), nil, nil)
	require.NoError(t, err)

	assert.Empty(t, config.AuthConfig.Token)
	assert.Empty(t, config.AuthConfig.WorkspaceID)
	assert.Empty(t, config.ExternalAppID)
	assert.Empty(t, config.ExternalBuildID)
	assert.Empty(t, config.ExternalWorkflowName)
	assert.True(t, config.Lite)

	body, err := json.Marshal(config)
	require.NoError(t, err)
	for name, value := range envs {
		if len(value) < 8 {
			continue
		}
		assert.NotContains(t, string(body), value, "%s leaked into the config", name)
	}
}

// $TMPDIR is per-session on macOS, so warmup's socket path is not the one the
// build's proxy binds. Persisting it points COMPILATION_CACHE_REMOTE_SERVICE_PATH
// at a dead socket and every lookup silently misses.
func TestNewConfig_LitePersistsNoProxySocketPath(t *testing.T) {
	isolate(t)

	osProxyMock := &utilsMocks.OsProxyMock{
		TempDirFunc:  func() string { return "/warmup-tmp" },
		HostnameFunc: func() (string, error) { return "test-host", nil },
	}

	config, err := NewConfig(t.Context(), liteTestLogger(), Params{BuildCacheEnabled: true, Lite: true},
		map[string]string{}, osProxyMock, whichCommandFunc(t), nil, nil)
	require.NoError(t, err)

	assert.Empty(t, config.ProxySocketPath, "warmup's $TMPDIR is not the build's")
}

// ...and the readers must still get a usable one, resolved in their own
// environment rather than read out of the file.
func TestReadConfig_ResolvesAnAbsentProxySocketPath(t *testing.T) {
	home := isolate(t)

	dir := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"proxySocketPath":""}`), 0o600))

	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		OpenFileFunc:    utils.DefaultOsProxy{}.OpenFile,
		TempDirFunc:     func() string { return "/build-tmp" },
	}

	config, err := ReadConfig(osProxy, utils.DefaultDecoderFactory{}, map[string]string{})
	require.NoError(t, err)

	assert.Equal(t, ResolveProxySocketPath("", map[string]string{}, osProxy), config.ProxySocketPath)
	assert.NotEmpty(t, config.ProxySocketPath)
}

// A lite config is "no usable config" for every purpose, the PATH safety net
// included. Skipping it lets `which xcodebuild` resolve to the wrapper, which
// activation then persists as the original — and the wrapper execs itself.
func TestOverrideActivateXcodeParamsFromExistingConfig_LiteStillFallsBackToUsrBin(t *testing.T) {
	home := isolate(t)

	dir := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"lite":true,"originalXcodebuildPath":"/warmup/xcodebuild"}`), 0o600))

	osProxy := utils.DefaultOsProxy{}
	envs := map[string]string{"PATH": PathFor(osProxy, BinDir) + ":/usr/bin"}

	params := Params{}
	overrideActivateXcodeParamsFromExistingConfig(
		liteTestLogger(), osProxy, &params, utils.DefaultDecoderFactory{}, envs)

	assert.Equal(t, "/usr/bin/xcodebuild", params.XcodePathOverride)
}

// The carry-forward itself still has to be skipped for a lite config.
func TestOverrideActivateXcodeParamsFromExistingConfig_LiteDoesNotCarryPathsForward(t *testing.T) {
	home := isolate(t)

	dir := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"lite":true,"originalXcodebuildPath":"/warmup/xcodebuild"}`), 0o600))

	params := Params{}
	overrideActivateXcodeParamsFromExistingConfig(
		liteTestLogger(), utils.DefaultOsProxy{}, &params, utils.DefaultDecoderFactory{},
		map[string]string{"PATH": "/usr/bin"})

	assert.Empty(t, params.XcodePathOverride, "a warmup config resolved its paths before the stack was selected")
}
