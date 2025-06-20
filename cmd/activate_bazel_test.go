package cmd

import (
	"errors"
	"testing"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/bazel"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_activateBazelCmdFn(t *testing.T) {
	prep := func() log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	t.Run("When no error activateBazelCmdFn creates template inventory and writes bazelrc", func(t *testing.T) {
		mockLogger := prep()
		templateInventory := bazelconfig.TemplateInventory{
			Common: bazelconfig.CommonTemplateInventory{
				AuthToken: "AuthTokenValue",
			},
		}

		var actualTemplateInventory *bazelconfig.TemplateInventory
		var actualPath *string

		// when
		err := activateBazelCmdFn(
			mockLogger,
			"~/.bazelrc",
			func(string) string { return "" },
			func(log.Logger, func(string) string, bool) (bazelconfig.TemplateInventory, error) {
				return templateInventory, nil
			},
			func(
				inventory bazelconfig.TemplateInventory,
				path string,
			) error {
				actualTemplateInventory = &inventory
				actualPath = &path

				return nil
			},
		)

		// then
		require.NoError(t, err)
		require.Equal(t, templateInventory, *actualTemplateInventory)
		require.Equal(t, "~/.bazelrc", *actualPath)
	})

	t.Run("When templateInventory creation fails activateBazelCmdFn throws error", func(t *testing.T) {
		mockLogger := prep()
		inventoryCreationError := errors.New("failed to create inventory")

		// when
		err := activateBazelCmdFn(
			mockLogger,
			"~/.bazelrc",
			func(string) string { return "" },
			func(log.Logger, func(string) string, bool) (bazelconfig.TemplateInventory, error) {
				return bazelconfig.TemplateInventory{}, inventoryCreationError
			},
			func(
				bazelconfig.TemplateInventory,
				string,
			) error {
				return nil
			},
		)

		// then
		require.EqualError(t, err, inventoryCreationError.Error())
	})

	t.Run("When template writing fails activateBazelCmdFn throws error", func(t *testing.T) {
		mockLogger := prep()
		templateWriteError := errors.New("failed to write template")

		// when
		err := activateBazelCmdFn(
			mockLogger,
			"~/.bazelrc",
			func(string) string { return "" },
			func(log.Logger, func(string) string, bool) (bazelconfig.TemplateInventory, error) {
				return bazelconfig.TemplateInventory{}, nil
			},
			func(
				bazelconfig.TemplateInventory,
				string,
			) error {
				return templateWriteError
			},
		)

		// then
		require.EqualError(t, err, templateWriteError.Error())
	})
}
