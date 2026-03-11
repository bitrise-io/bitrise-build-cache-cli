//go:build unit

package ccache_test

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cmdccache "github.com/bitrise-io/bitrise-build-cache-cli/cmd/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
)

func validEnvs() map[string]string {
	return map[string]string{
		"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "test-token",
	}
}

func newOsProxyMock(t *testing.T) *mocks.OsProxyMock {
	t.Helper()
	tmpDir := t.TempDir()
	return &mocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return tmpDir, nil },
		MkdirAllFunc:   func(_ string, _ os.FileMode) error { return nil },
		GetwdFunc:      func() (string, error) { return "/work/dir", nil },
		CreateFunc: func(_ string) (*os.File, error) {
			return os.CreateTemp(tmpDir, "ccache-config-*.json")
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

// trackingCommandFunc returns a CommandFunc that records envman key/value pairs set via it.
func trackingCommandFunc() (utils.CommandFunc, map[string]string) {
	envVars := map[string]string{}
	cmdFunc := func(_ context.Context, name string, args ...string) utils.Command {
		// envman add --key <key> --value <value>
		if name == "envman" && len(args) == 5 && args[0] == "add" && args[1] == "--key" && args[3] == "--value" {
			envVars[args[2]] = args[4]
		}
		return &mocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) { return nil, nil },
		}
	}
	return cmdFunc, envVars
}

func Test_ActivateCppCommandFn(t *testing.T) {
	t.Run("sets all required environment variables via envman", func(t *testing.T) {
		cmdFunc, envVars := trackingCommandFunc()

		err := cmdccache.ActivateCppCommandFn(
			context.Background(),
			mockLogger,
			newOsProxyMock(t),
			cmdFunc,
			noOpEncoderFactory(),
			ccacheconfig.DefaultParams(),
			validEnvs(),
		)

		require.NoError(t, err)
		assert.Equal(t, "/work/dir", envVars["CCACHE_BASEDIR"])
		assert.Equal(t, "true", envVars["CCACHE_NOHASHDIR"])
		assert.Equal(t, "true", envVars["CCACHE_REMOTE_ONLY"])
		assert.Equal(t, "ccache", envVars["CMAKE_CXX_COMPILER_LAUNCHER"])
		assert.Equal(t, "ccache", envVars["CMAKE_C_COMPILER_LAUNCHER"])
		assert.Contains(t, envVars["CCACHE_REMOTE_STORAGE"], "crsh:")
	})

	t.Run("uses BaseDirOverride when provided", func(t *testing.T) {
		cmdFunc, envVars := trackingCommandFunc()
		params := ccacheconfig.DefaultParams()
		params.BaseDirOverride = "/custom/basedir"

		err := cmdccache.ActivateCppCommandFn(
			context.Background(),
			mockLogger,
			newOsProxyMock(t),
			cmdFunc,
			noOpEncoderFactory(),
			params,
			validEnvs(),
		)

		require.NoError(t, err)
		assert.Equal(t, "/custom/basedir", envVars["CCACHE_BASEDIR"])
	})

	t.Run("uses Getwd for CCACHE_BASEDIR when no BaseDirOverride", func(t *testing.T) {
		cmdFunc, envVars := trackingCommandFunc()
		osProxy := newOsProxyMock(t)
		osProxy.GetwdFunc = func() (string, error) { return "/from/getwd", nil }

		err := cmdccache.ActivateCppCommandFn(
			context.Background(),
			mockLogger,
			osProxy,
			cmdFunc,
			noOpEncoderFactory(),
			ccacheconfig.DefaultParams(),
			validEnvs(),
		)

		require.NoError(t, err)
		assert.Equal(t, "/from/getwd", envVars["CCACHE_BASEDIR"])
	})

	t.Run("CCACHE_REMOTE_STORAGE contains IPC socket path override", func(t *testing.T) {
		cmdFunc, envVars := trackingCommandFunc()
		params := ccacheconfig.DefaultParams()
		params.IPCSocketPathOverride = "/custom/ccache.sock"

		err := cmdccache.ActivateCppCommandFn(
			context.Background(),
			mockLogger,
			newOsProxyMock(t),
			cmdFunc,
			noOpEncoderFactory(),
			params,
			validEnvs(),
		)

		require.NoError(t, err)
		assert.Contains(t, envVars["CCACHE_REMOTE_STORAGE"], "/custom/ccache.sock")
	})

	t.Run("CCACHE_BASEDIR is empty when Getwd fails and no override", func(t *testing.T) {
		cmdFunc, envVars := trackingCommandFunc()
		osProxy := newOsProxyMock(t)
		osProxy.GetwdFunc = func() (string, error) { return "", errors.New("getwd failed") }

		err := cmdccache.ActivateCppCommandFn(
			context.Background(),
			mockLogger,
			osProxy,
			cmdFunc,
			noOpEncoderFactory(),
			ccacheconfig.DefaultParams(),
			validEnvs(),
		)

		require.NoError(t, err)
		assert.Equal(t, "", envVars["CCACHE_BASEDIR"])
	})

	t.Run("returns error when auth config is missing", func(t *testing.T) {
		noOpCmd := func(_ context.Context, _ string, _ ...string) utils.Command {
			return &mocks.CommandMock{CombinedOutputFunc: func() ([]byte, error) { return nil, nil }}
		}

		err := cmdccache.ActivateCppCommandFn(
			context.Background(),
			mockLogger,
			newOsProxyMock(t),
			noOpCmd,
			noOpEncoderFactory(),
			ccacheconfig.DefaultParams(),
			map[string]string{},
		)

		assert.ErrorContains(t, err, "failed to create ccache config")
	})

	t.Run("returns error when config save fails", func(t *testing.T) {
		osProxy := newOsProxyMock(t)
		osProxy.CreateFunc = func(_ string) (*os.File, error) {
			return nil, os.ErrPermission
		}
		noOpCmd := func(_ context.Context, _ string, _ ...string) utils.Command {
			return &mocks.CommandMock{CombinedOutputFunc: func() ([]byte, error) { return nil, nil }}
		}

		err := cmdccache.ActivateCppCommandFn(
			context.Background(),
			mockLogger,
			osProxy,
			noOpCmd,
			noOpEncoderFactory(),
			ccacheconfig.DefaultParams(),
			validEnvs(),
		)

		assert.ErrorContains(t, err, "failed to save ccache config")
	})
}
