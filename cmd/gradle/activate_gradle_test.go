//nolint:dupl
package gradle_test

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/gradle"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
)

func Test_activateGradleCmdFn(t *testing.T) {
	t.Run("When no error activateGradleCmdFn creates template inventory and writes gradle config file", func(t *testing.T) {
		templateInventory := gradleconfig.TemplateInventory{
			Common: gradleconfig.PluginCommonTemplateInventory{
				AppSlug: "AppSlugValue",
			},
		}

		var actualTemplateInventory *gradleconfig.TemplateInventory

		mockOsProxy := &mocks.OsProxyMock{
			ReadFileIfExistsFunc: func(_ string) (string, bool, error) {
				return "", false, nil
			},
			WriteFileFunc: func(_ string, _ []byte, _ os.FileMode) error {
				return nil
			},
		}

		// when
		err := gradle.ActivateGradleCmdFn(
			mockLogger,
			"~/.gradle",
			map[string]string{},
			func(log.Logger, map[string]string, bool) (gradleconfig.TemplateInventory, error) {
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
		assert.Equal(t, templateInventory, *actualTemplateInventory)
		require.Len(t, mockOsProxy.ReadFileIfExistsCalls(), 1)
		require.Equal(t, "~/.gradle/gradle.properties", mockOsProxy.ReadFileIfExistsCalls()[0].Name)
	})

	t.Run("When templateInventory creation fails activateGradleCmdFn throws error", func(t *testing.T) {
		inventoryCreationError := errors.New("failed to create inventory")

		mockOsProxy := &mocks.OsProxyMock{
			ReadFileIfExistsFunc: func(_ string) (string, bool, error) {
				return "", true, nil
			},
			WriteFileFunc: func(_ string, _ []byte, _ os.FileMode) error {
				return nil
			},
		}

		// when
		err := gradle.ActivateGradleCmdFn(
			mockLogger,
			"~/.gradle",
			map[string]string{},
			func(log.Logger, map[string]string, bool) (gradleconfig.TemplateInventory, error) {
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
		assert.EqualError(t, err, inventoryCreationError.Error())
	})

	t.Run("When template writing fails activateGradleCmdFn throws error", func(t *testing.T) {
		templateWriteError := errors.New("failed to write template")

		mockOsProxy := &mocks.OsProxyMock{
			ReadFileIfExistsFunc: func(_ string) (string, bool, error) {
				return "", true, nil
			},
			WriteFileFunc: func(_ string, _ []byte, _ os.FileMode) error {
				return nil
			},
		}

		// when
		err := gradle.ActivateGradleCmdFn(
			mockLogger,
			"~/.gradle",
			map[string]string{},
			func(log.Logger, map[string]string, bool) (gradleconfig.TemplateInventory, error) {
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
		assert.EqualError(t, err, templateWriteError.Error())
	})

	t.Run("When gradle.property update fails activateGradleCmdFn throws error", func(t *testing.T) {
		gradlePropertiesUpdateError := errors.New("failed to update gradle.properties")

		mockOsProxy := &mocks.OsProxyMock{
			ReadFileIfExistsFunc: func(_ string) (string, bool, error) {
				return "", true, nil
			},
			WriteFileFunc: func(_ string, _ []byte, _ os.FileMode) error {
				return gradlePropertiesUpdateError
			},
		}

		// when
		err := gradle.ActivateGradleCmdFn(
			mockLogger,
			"~/.gradle",
			map[string]string{},
			func(log.Logger, map[string]string, bool) (gradleconfig.TemplateInventory, error) {
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
		assert.EqualError(
			t,
			err,
			fmt.Errorf(
				gradle.ErrFmtFailedToUpdateProps,
				fmt.Errorf(
					gradleconfig.ErrFmtGradlePropertyWrite,
					"~/.gradle/gradle.properties",
					gradlePropertiesUpdateError,
				),
			).Error(),
		)
	})
}
