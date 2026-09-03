//go:build unit

package ccache_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
)

var mockLogger = newMockLogger() //nolint:gochecknoglobals

func init() { keyring.MockInit() }

func newMockLogger() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	l.On("Debugf", mock.Anything, mock.Anything).Return()
	l.On("EnableDebugLog", mock.Anything).Return()
	l.On("Infof", mock.Anything, mock.Anything).Return()
	l.On("Infof", mock.Anything).Return()
	l.On("TInfof", mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything, mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything).Return()
	l.On("Warnf", mock.Anything, mock.Anything).Return()

	return l
}

func validEnvs() map[string]string {
	return map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "test-token",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "test-workspace",
	}
}

func newOsProxyMock(t *testing.T) *mocks.OsProxyMock {
	t.Helper()
	tmpDir := t.TempDir()

	return &mocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return tmpDir, nil },
		MkdirAllFunc:    func(_ string, _ os.FileMode) error { return nil },
		GetwdFunc:       func() (string, error) { return "/work/dir", nil },
		TempDirFunc:     func() string { return tmpDir },
		CreateFunc: func(_ string) (*os.File, error) {
			return os.CreateTemp(tmpDir, "ccache-config-*.json")
		},
		WriteFileFunc: func(_ string, _ []byte, _ os.FileMode) error { return nil },
		RenameFunc:    func(_, _ string) error { return nil },
		RemoveFunc:    func(_ string) error { return nil },
		// The analytics config is now read-modify-write, so activation opens it
		// before saving. Absent is the normal first-activation case.
		OpenFileFunc: func(_ string, _ int, _ os.FileMode) (*os.File, error) {
			return nil, fs.ErrNotExist
		},
	}
}

func noOpEncoderFactory() *mocks.EncoderFactoryMock {
	return &mocks.EncoderFactoryMock{
		EncoderFunc: func(_ io.Writer) utils.Encoder {
			return &mocks.EncoderMock{
				SetIndentFunc:     func(_ string, _ string) {},
				SetEscapeHTMLFunc: func(_ bool) {},
				EncodeFunc:        func(_ any) error { return nil },
			}
		},
	}
}

func trackingCommandFunc() (utils.CommandFunc, map[string]string) {
	envVars := map[string]string{}
	cmdFunc := func(_ context.Context, name string, args ...string) utils.Command {
		if name == "envman" && len(args) == 5 && args[0] == "add" && args[1] == "--key" && args[3] == "--value" {
			envVars[args[2]] = args[4]
		}

		return &mocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) { return nil, nil },
		}
	}

	return cmdFunc, envVars
}

func newTestActivator(t *testing.T, params ccachepkg.ActivatorParams) (*ccachepkg.Activator, map[string]string) {
	t.Helper()
	cmdFunc, envVars := trackingCommandFunc()

	if params.Logger == nil {
		params.Logger = mockLogger
	}

	if params.OsProxy == nil {
		params.OsProxy = newOsProxyMock(t)
	}

	params.CommandFunc = cmdFunc
	params.EncoderFactory = noOpEncoderFactory()

	return ccachepkg.NewActivator(params), envVars
}

func TestActivator_Activate(t *testing.T) {
	t.Run("sets all required environment variables via envman", func(t *testing.T) {
		a, envVars := newTestActivator(t, ccachepkg.ActivatorParams{
			PushEnabled: ccacheconfig.DefaultParams().PushEnabled,
			Envs:        validEnvs(),
		})

		err := a.Activate(context.Background())

		require.NoError(t, err)
		assert.Equal(t, "/work/dir", envVars["CCACHE_BASEDIR"])
		assert.Equal(t, "content", envVars["CCACHE_COMPILERCHECK"])
		assert.Equal(t, "true", envVars["CCACHE_NOHASHDIR"])
		assert.Equal(t, "true", envVars["CCACHE_REMOTE_ONLY"])
		assert.Equal(t, "ccache", envVars["CMAKE_CXX_COMPILER_LAUNCHER"])
		assert.Equal(t, "ccache", envVars["CMAKE_C_COMPILER_LAUNCHER"])
		assert.Contains(t, envVars["CCACHE_REMOTE_STORAGE"], "crsh:")
	})

	t.Run("creates the log dir the storage helper would create on its first run", func(t *testing.T) {
		osProxy := newOsProxyMock(t)
		home, err := osProxy.UserHomeDir()
		require.NoError(t, err)

		a, _ := newTestActivator(t, ccachepkg.ActivatorParams{
			PushEnabled: ccacheconfig.DefaultParams().PushEnabled,
			Envs:        validEnvs(),
			OsProxy:     osProxy,
		})

		require.NoError(t, a.Activate(context.Background()))

		var created []string
		for _, c := range osProxy.MkdirAllCalls() {
			created = append(created, c.Name)
		}
		assert.Contains(t, created, paths.FromHome(home).CcacheLogDir())
	})

	t.Run("uses BaseDirOverride when provided", func(t *testing.T) {
		a, envVars := newTestActivator(t, ccachepkg.ActivatorParams{
			PushEnabled:     ccacheconfig.DefaultParams().PushEnabled,
			BaseDirOverride: "/custom/basedir",
			Envs:            validEnvs(),
		})

		err := a.Activate(context.Background())

		require.NoError(t, err)
		assert.Equal(t, "/custom/basedir", envVars["CCACHE_BASEDIR"])
	})

	t.Run("uses Getwd for CCACHE_BASEDIR when no BaseDirOverride", func(t *testing.T) {
		osProxy := newOsProxyMock(t)
		osProxy.GetwdFunc = func() (string, error) { return "/from/getwd", nil }

		a, envVars := newTestActivator(t, ccachepkg.ActivatorParams{
			PushEnabled: ccacheconfig.DefaultParams().PushEnabled,
			Envs:        validEnvs(),
			OsProxy:     osProxy,
		})

		err := a.Activate(context.Background())

		require.NoError(t, err)
		assert.Equal(t, "/from/getwd", envVars["CCACHE_BASEDIR"])
	})

	t.Run("CCACHE_REMOTE_STORAGE contains IPC socket path override", func(t *testing.T) {
		a, envVars := newTestActivator(t, ccachepkg.ActivatorParams{
			PushEnabled:           ccacheconfig.DefaultParams().PushEnabled,
			IPCSocketPathOverride: "/custom/ccache.sock",
			Envs:                  validEnvs(),
		})

		err := a.Activate(context.Background())

		require.NoError(t, err)
		assert.Contains(t, envVars["CCACHE_REMOTE_STORAGE"], "/custom/ccache.sock")
	})

	t.Run("CCACHE_BASEDIR is empty when Getwd fails and no override", func(t *testing.T) {
		osProxy := newOsProxyMock(t)
		osProxy.GetwdFunc = func() (string, error) { return "", errors.New("getwd failed") }

		a, envVars := newTestActivator(t, ccachepkg.ActivatorParams{
			PushEnabled: ccacheconfig.DefaultParams().PushEnabled,
			Envs:        validEnvs(),
			OsProxy:     osProxy,
		})

		err := a.Activate(context.Background())

		require.NoError(t, err)
		assert.Equal(t, "", envVars["CCACHE_BASEDIR"])
	})

	t.Run("returns error when auth config is missing", func(t *testing.T) {
		// Earlier subtests persist credentials into the shared mock keyring, and
		// ResolveAuthConfig would find them — reset so "missing" really is missing.
		keyring.MockInit()

		a, _ := newTestActivator(t, ccachepkg.ActivatorParams{
			PushEnabled: ccacheconfig.DefaultParams().PushEnabled,
			Envs:        map[string]string{},
		})

		err := a.Activate(context.Background())
		assert.ErrorContains(t, err, "failed to create ccache config")
	})

	t.Run("returns error when config save fails", func(t *testing.T) {
		osProxy := newOsProxyMock(t)
		osProxy.CreateFunc = func(_ string) (*os.File, error) {
			return nil, os.ErrPermission
		}

		a, _ := newTestActivator(t, ccachepkg.ActivatorParams{
			PushEnabled: ccacheconfig.DefaultParams().PushEnabled,
			Envs:        validEnvs(),
			OsProxy:     osProxy,
		})

		err := a.Activate(context.Background())
		assert.ErrorContains(t, err, "failed to save ccache config")
	})
}
