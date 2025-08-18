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
