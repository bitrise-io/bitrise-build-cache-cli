package gradleconfig

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_writeGradleInitGradle(t *testing.T) {
	prep := func() (log.Logger, string) {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
		tmpPath := t.TempDir()
		tmpGradleHomeDir := filepath.Join(tmpPath, ".gradle")

		return mockLogger, tmpGradleHomeDir
	}

	t.Run("writes the gradle init file", func(t *testing.T) {
		mockLogger, tmpGradleHomeDir := prep()

		inventory := TemplateInventory{}

		// when
		err := inventory.WriteToGradleInit(mockLogger, tmpGradleHomeDir, utils.DefaultOsProxy{}, GradleTemplateProxy())

		// then
		require.NoError(t, err)
		//
		isInitFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
		require.NoError(t, err)
		assert.True(t, isInitFileExists)
	})

	t.Run("when can't make directories throws error", func(t *testing.T) {
		mockLogger, tmpGradleHomeDir := prep()

		inventory := TemplateInventory{}
		expectedError := errors.New("failed to create directories")
		osProxy := &utilsMocks.MockOsProxy{}
		osProxy.On("MkdirAll", mock.Anything, mock.Anything).Return(expectedError)

		// when
		err := inventory.WriteToGradleInit(mockLogger, tmpGradleHomeDir, osProxy, GradleTemplateProxy())

		// then
		require.ErrorIs(t, err, expectedError)
	})

	t.Run("when template parsing fails throws error", func(t *testing.T) {
		mockLogger, tmpGradleHomeDir := prep()

		inventory := TemplateInventory{}
		expectedError := errors.New("failed to parse template")
		templateProxy := utils.TemplateProxy{
			Parse: func(string, string) (*template.Template, error) {
				return nil, expectedError
			},
		}

		// when
		err := inventory.WriteToGradleInit(mockLogger, tmpGradleHomeDir, utils.DefaultOsProxy{}, templateProxy)

		// then
		require.ErrorIs(t, err, expectedError)
	})

	t.Run("when template execution fails throws error", func(t *testing.T) {
		mockLogger, tmpGradleHomeDir := prep()

		inventory := TemplateInventory{}
		expectedError := errors.New("failed to execute template")
		templateProxy := utils.TemplateProxy{
			Parse: GradleTemplateProxy().Parse,
			Execute: func(*template.Template, *bytes.Buffer, interface{}) error {
				return expectedError
			},
		}

		// when
		err := inventory.WriteToGradleInit(mockLogger, tmpGradleHomeDir, utils.DefaultOsProxy{}, templateProxy)

		// then
		require.ErrorIs(t, err, expectedError)
	})

	t.Run("when writing init.gradle fails throws error", func(t *testing.T) {
		mockLogger, tmpGradleHomeDir := prep()

		inventory := TemplateInventory{}
		expectedError := errors.New("failed to write init.gradle")
		osProxy := &utilsMocks.MockOsProxy{}
		osProxy.On("MkdirAll", mock.Anything, mock.Anything).Return(nil)
		osProxy.On("WriteFile", mock.Anything, mock.Anything, mock.Anything).Return(expectedError)

		// when
		err := inventory.WriteToGradleInit(mockLogger, tmpGradleHomeDir, osProxy, GradleTemplateProxy())

		// then
		require.ErrorIs(t, err, expectedError)
	})
}
