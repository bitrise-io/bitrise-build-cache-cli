//nolint:dupl
package cmd

import (
	"errors"
	"fmt"
	"testing"

	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_activateGradleCmdFn(t *testing.T) {
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

	t.Run("When no error activateGradleCmdFn creates template inventory and writes gradle config file", func(t *testing.T) {
		mockLogger := prep()
		templateInventory := gradleconfig.TemplateInventory{
			Common: gradleconfig.PluginCommonTemplateInventory{
				AppSlug: "AppSlugValue",
			},
		}

		var actualTemplateInventory *gradleconfig.TemplateInventory

		mockOsProxy := &utilsMocks.MockOsProxy{}
		mockOsProxy.On("ReadFileIfExists", "~/.gradle/gradle.properties").Return("", true, nil)
		mockOsProxy.On("WriteFile", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		// when
		err := activateGradleCmdFn(
			mockLogger,
			"~/.gradle",
			func(string) string { return "" },
			func(log.Logger, func(string) string, bool) (gradleconfig.TemplateInventory, error) {
				return templateInventory, nil
			},
			func(
				inventory gradleconfig.TemplateInventory,
				_ string,
			) error {
				actualTemplateInventory = &inventory

				return nil
			},
			gradleconfig.GradlePropertiesUpdater{
				OsProxy: mockOsProxy,
			},
		)

		// then
		require.NoError(t, err)
		require.Equal(t, templateInventory, *actualTemplateInventory)
		mockOsProxy.AssertCalled(t, "ReadFileIfExists", "~/.gradle/gradle.properties")
	})

	t.Run("When templateInventory creation fails activateGradleCmdFn throws error", func(t *testing.T) {
		mockLogger := prep()
		inventoryCreationError := errors.New("failed to create inventory")

		mockOsProxy := &utilsMocks.MockOsProxy{}
		mockOsProxy.On("ReadFileIfExists", "~/.gradle/gradle.properties").Return("", true, nil)
		mockOsProxy.On("WriteFile", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		// when
		err := activateGradleCmdFn(
			mockLogger,
			"~/.gradle",
			func(string) string { return "" },
			func(log.Logger, func(string) string, bool) (gradleconfig.TemplateInventory, error) {
				return gradleconfig.TemplateInventory{}, inventoryCreationError
			},
			func(
				gradleconfig.TemplateInventory,
				string,
			) error {
				return nil
			},
			gradleconfig.GradlePropertiesUpdater{
				OsProxy: mockOsProxy,
			},
		)

		// then
		require.EqualError(t, err, inventoryCreationError.Error())
	})

	t.Run("When template writing fails activateGradleCmdFn throws error", func(t *testing.T) {
		mockLogger := prep()
		templateWriteError := errors.New("failed to write template")

		mockOsProxy := &utilsMocks.MockOsProxy{}
		mockOsProxy.On("ReadFileIfExists", "~/.gradle/gradle.properties").Return("", true, nil)
		mockOsProxy.On("WriteFile", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		// when
		err := activateGradleCmdFn(
			mockLogger,
			"~/.gradle",
			func(string) string { return "" },
			func(log.Logger, func(string) string, bool) (gradleconfig.TemplateInventory, error) {
				return gradleconfig.TemplateInventory{}, nil
			},
			func(
				gradleconfig.TemplateInventory,
				string,
			) error {
				return templateWriteError
			},
			gradleconfig.GradlePropertiesUpdater{
				OsProxy: mockOsProxy,
			},
		)

		// then
		require.EqualError(t, err, templateWriteError.Error())
	})

	t.Run("When gradle.property update fails activateGradleCmdFn throws error", func(t *testing.T) {
		mockLogger := prep()
		gradlePropertiesUpdateError := errors.New("failed to update gradle.properties")

		mockOsProxy := &utilsMocks.MockOsProxy{}
		mockOsProxy.On("ReadFileIfExists", mock.Anything).Return("", true, nil)
		mockOsProxy.On("WriteFile", mock.Anything, mock.Anything, mock.Anything).Return(gradlePropertiesUpdateError)

		// when
		err := activateGradleCmdFn(
			mockLogger,
			"~/.gradle",
			func(string) string { return "" },
			func(log.Logger, func(string) string, bool) (gradleconfig.TemplateInventory, error) {
				return gradleconfig.TemplateInventory{}, nil
			},
			func(
				gradleconfig.TemplateInventory,
				string,
			) error {
				return nil
			},
			gradleconfig.GradlePropertiesUpdater{
				OsProxy: mockOsProxy,
			},
		)

		// then
		require.EqualError(
			t,
			err,
			fmt.Errorf(
				errFmtFailedToUpdateProps,
				fmt.Errorf(
					gradleconfig.ErrFmtGradlePropertyWrite,
					"~/.gradle/gradle.properties",
					gradlePropertiesUpdateError,
				),
			).Error(),
		)
	})
}
