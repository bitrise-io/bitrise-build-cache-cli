//nolint:dupl
package cmd_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	cmdMocks "github.com/bitrise-io/bitrise-build-cache-cli/cmd/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
)

func TestActivateXcode_activateXcodeCmdFn(t *testing.T) {
	config := &cmdMocks.XcelerateConfigMock{}

	home := os.TempDir()

	osProxy := &utilsMocks.OsProxyMock{
		ReadFileIfExistsFunc: utils.DefaultOsProxy{}.ReadFileIfExists,
		UserHomeDirFunc: func() (string, error) {
			return home, nil
		},
		MkdirAllFunc:  os.MkdirAll,
		CreateFunc:    os.Create,
		OpenFileFunc:  os.OpenFile,
		WriteFileFunc: os.WriteFile,
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
			context.Background(),
			mockLogger,
			osProxy,
			func(ctx context.Context, command string, args ...string) utils.Command {
				return &utilsMocks.CommandMock{}
			},
			encoderFactory(),
			config,
			map[string]string{},
		)

		mockLogger.AssertCalled(t, "TInfof", cmd.ActivateXcodeSuccessful)
		require.NoError(t, err)

		// make sure files were created
		assert.FileExists(t, filepath.Join(home, ".bashrc"))
		assert.FileExists(t, filepath.Join(home, ".zshrc"))
		assert.FileExists(t, xcelerate.PathFor(osProxy, filepath.Join(xcelerate.BinDir, "xcodebuild")))

		// make sure config save was called
		require.Len(t, config.SaveCalls(), 1)
	})

	t.Run("When config save returns error activateXcodeCmdFn fails", func(t *testing.T) {
		expectedError := errors.New("failed to save config")

		mockConfig := &cmdMocks.XcelerateConfigMock{
			SaveFunc: func(_ log.Logger, _ utils.OsProxy, _ utils.EncoderFactory) error {
				return expectedError
			},
		}

		err := cmd.ActivateXcodeCommandFn(
			context.Background(),
			mockLogger,
			osProxy,
			func(ctx context.Context, command string, args ...string) utils.Command {
				return &utilsMocks.CommandMock{}
			},
			encoderFactory(),
			mockConfig,
			map[string]string{},
		)

		assert.ErrorContains(t, err, fmt.Errorf(cmd.ErrFmtCreateXcodeConfig, expectedError).Error())
	})
}
