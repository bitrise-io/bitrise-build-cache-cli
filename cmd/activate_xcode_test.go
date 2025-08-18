//nolint:dupl
package cmd_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"

	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	cmdMocks "github.com/bitrise-io/bitrise-build-cache-cli/cmd/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivateXcode_activateXcodeCmdFn(t *testing.T) {
	config := func() *cmdMocks.XcelerateConfigMock {
		return &cmdMocks.XcelerateConfigMock{
			SaveFunc: func(_ utils.OsProxy, _ utils.EncoderFactory) error {
				return nil
			},
		}
	}

	osProxy := func() *utilsMocks.OsProxyMock {
		return &utilsMocks.OsProxyMock{
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) { return "", true, nil },
			UserHomeDirFunc: func() (string, error) {
				return "~", nil
			},
			MkdirAllFunc: func(_ string, _ os.FileMode) error {
				return nil
			},
			CreateFunc: func(_ string) (*os.File, error) {
				return &os.File{}, nil
			},
			ExecutableFunc: func() (string, error) { return "exe", nil },
			OpenFileFunc: func(name string, flag int, perm os.FileMode) (*os.File, error) {
				return &os.File{}, nil
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return nil
			},
		}
	}

	encoder := func() *utilsMocks.EncoderMock {
		return &utilsMocks.EncoderMock{
			SetIndentFunc:     func(_ string, _ string) {},
			SetEscapeHTMLFunc: func(_ bool) {},
			EncodeFunc:        func(_ any) error { return nil },
		}
	}

	encoderFactory := func() *utilsMocks.EncoderFactoryMock {
		return &utilsMocks.EncoderFactoryMock{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return encoder()
			},
		}
	}

	t.Run("When no error activateXcodeCmdFn logs success", func(t *testing.T) {
		err := cmd.ActivateXcodeCommandFn(
			mockLogger,
			osProxy(),
			encoderFactory(),
			config(),
			func(path string, command string) cmd.Command {
				return &cmdMocks.CommandMock{
					SetSysProcAttrFunc: func(_ *syscall.SysProcAttr) {},
					SetStderrFunc:      func(_ *os.File) {},
					SetStdoutFunc:      func(_ *os.File) {},
					SetStdinFunc:       func(_ *os.File) {},
					PIDFunc:            func() int { return 444 },
					StartFunc:          func() error { return nil },
				}
			},
			func(pid int, signum syscall.Signal) {},
			func(string) string { return "" }, // envProvider
		)

		mockLogger.AssertCalled(t, "TInfof", cmd.ActivateXcodeSuccessful)
		require.NoError(t, err)
	})

	t.Run("When config save returns error activateXcodeCmdFn fails", func(t *testing.T) {
		expectedError := errors.New("failed to save config")

		mockConfig := &cmdMocks.XcelerateConfigMock{
			SaveFunc: func(_ utils.OsProxy, _ utils.EncoderFactory) error {
				return expectedError
			},
		}

		err := cmd.ActivateXcodeCommandFn(
			mockLogger,
			osProxy(),
			encoderFactory(),
			mockConfig,
			func(path string, command string) cmd.Command {
				return &cmdMocks.CommandMock{}
			},
			func(pid int, signum syscall.Signal) {},
			func(string) string { return "" }, // envProvider
		)

		assert.ErrorContains(t, err, fmt.Errorf(cmd.ErrFmtCreateXcodeConfig, expectedError).Error())
	})
}

func TestActivateXcode_addContentOrCreateFile(t *testing.T) {
	t.Run("When file does not exist, it creates the file with content", func(t *testing.T) {
		osProxy := &utilsMocks.OsProxyMock{
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
				if strings.Contains(pth, "test.txt") {
					return "", false, os.ErrNotExist // simulate file does not exist
				}

				return "something", false, nil
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return nil
			},
		}

		err := cmd.AddContentOrCreateFile(
			mockLogger,
			osProxy,
			"test.txt",
			"Bitrise Xcelerate",
			"export PATH=/path/to/xcelerate:$PATH",
		)

		require.NoError(t, err)
		require.Len(t, osProxy.ReadFileIfExistsCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.ReadFileIfExistsCalls()[0].Pth)
		require.Len(t, osProxy.WriteFileCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.WriteFileCalls()[0].Pth)
		assert.Equal(t, "# [start] Bitrise Xcelerate\nexport PATH=/path/to/xcelerate:$PATH\n# [end] Bitrise Xcelerate\n", string(osProxy.WriteFileCalls()[0].Data))
	})

	t.Run("When file exists with existing content, it updates the block", func(t *testing.T) {
		osProxy := &utilsMocks.OsProxyMock{
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
				if strings.Contains(pth, "test.txt") {
					return "# [start] Bitrise Xcelerate\nold content\n# [end] Bitrise Xcelerate\n", true, nil
				}

				return "", false, os.ErrNotExist
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return nil
			},
		}

		err := cmd.AddContentOrCreateFile(
			mockLogger,
			osProxy,
			"test.txt",
			"Bitrise Xcelerate",
			"export PATH=/path/to/xcelerate:$PATH",
		)

		require.NoError(t, err)
		require.Len(t, osProxy.ReadFileIfExistsCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.ReadFileIfExistsCalls()[0].Pth)
		require.Len(t, osProxy.WriteFileCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.WriteFileCalls()[0].Pth)
		assert.Equal(t, "# [start] Bitrise Xcelerate\nexport PATH=/path/to/xcelerate:$PATH\n# [end] Bitrise Xcelerate\n", string(osProxy.WriteFileCalls()[0].Data))
	})

	t.Run("When file writing returns error, returns error", func(t *testing.T) {
		expectedError := errors.New("failed to write file")

		osProxy := &utilsMocks.OsProxyMock{
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
				if strings.Contains(pth, "test.txt") {
					return "", true, nil
				}

				return "", false, os.ErrNotExist
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return expectedError // simulate write error
			},
		}

		err := cmd.AddContentOrCreateFile(
			mockLogger,
			osProxy,
			"test.txt",
			"# Bitrise Xcelerate",
			"export PATH=/path/to/xcelerate:$PATH",
		)

		require.ErrorIs(t, err, expectedError)
		require.Len(t, osProxy.ReadFileIfExistsCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.ReadFileIfExistsCalls()[0].Pth)
		require.Len(t, osProxy.WriteFileCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.WriteFileCalls()[0].Pth)
	})
}

func TestActivateXcode_AddXcelerateCommandToPath(t *testing.T) {
	osProxy := &utilsMocks.OsProxyMock{
		ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
			return "", true, nil
		},
		UserHomeDirFunc: func() (string, error) {
			return "/home/user", nil
		},
		WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
			return nil
		},
	}

	t.Run("When adding xcelerate command to PATH succeeds", func(t *testing.T) {
		err := cmd.AddXcelerateCommandToPath(mockLogger, osProxy)

		require.NoError(t, err)
	})

	t.Run("When writing to .bashrc fails, returns error", func(t *testing.T) {
		expectedError := errors.New("failed to write .bashrc")

		osProxy.WriteFileFunc = func(pth string, data []byte, mode os.FileMode) error {
			if strings.Contains(pth, ".bashrc") {
				return expectedError
			}

			return nil
		}

		err := cmd.AddXcelerateCommandToPath(mockLogger, osProxy)

		require.ErrorIs(t, err, expectedError)
	})

	t.Run("When writing to .zshrc fails, returns error", func(t *testing.T) {
		expectedError := errors.New("failed to write .zshrc")

		osProxy.WriteFileFunc = func(pth string, data []byte, mode os.FileMode) error {
			if strings.Contains(pth, ".zshrc") {
				return expectedError
			}

			return nil
		}

		err := cmd.AddXcelerateCommandToPath(mockLogger, osProxy)

		require.ErrorIs(t, err, expectedError)
	})

	t.Run("When home directory cannot be determined, returns error", func(t *testing.T) {
		osProxy.UserHomeDirFunc = func() (string, error) {
			return "", errors.New("failed to get home directory")
		}

		err := cmd.AddXcelerateCommandToPath(mockLogger, osProxy)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get home directory")
	})
}
