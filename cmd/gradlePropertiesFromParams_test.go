package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_gradlePropertiesFromParams(t *testing.T) {
	prep := func() (log.Logger, string, string) {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()

		tmpPath := t.TempDir()
		tmpGradleHomeDir := filepath.Join(tmpPath, ".gradle")
		_ = os.MkdirAll(tmpGradleHomeDir, 0755)

		propertyFilePath := filepath.Join(tmpGradleHomeDir, "gradle.properties")
		os.Remove(propertyFilePath)
		file, _ := os.Create(propertyFilePath)
		defer file.Close()

		return mockLogger, tmpGradleHomeDir, propertyFilePath
	}

	t.Run("Update gradle properties", func(t *testing.T) {
		updater := gradlePropertiesUpdater{
			readFileIfExists,
			gradleconfig.DefaultOsProxy(),
		}

		mockLogger, tmpGradleHomeDir, propertyFilePath := prep()

		// when
		err := updater.updateGradleProps(
			ActivateForGradleParams{
				Cache: CacheParams{
					Enabled: true,
				},
			},
			mockLogger,
			tmpGradleHomeDir,
		)

		// then
		require.NoError(t, err)
		//
		data, err := os.ReadFile(propertyFilePath)
		require.NoError(t, err)
		content := string(data)
		assert.Contains(t, content, "org.gradle.caching=true")
	})

	t.Run("Update gradle properties when caching is disabled", func(t *testing.T) {
		updater := gradlePropertiesUpdater{
			readFileIfExists,
			gradleconfig.DefaultOsProxy(),
		}

		mockLogger, tmpGradleHomeDir, propertyFilePath := prep()

		// when
		err := updater.updateGradleProps(
			ActivateForGradleParams{
				Cache: CacheParams{
					Enabled: false,
				},
			},
			mockLogger,
			tmpGradleHomeDir,
		)

		// then
		require.NoError(t, err)
		//
		data, err := os.ReadFile(propertyFilePath)
		require.NoError(t, err)
		content := string(data)
		assert.Contains(t, content, "org.gradle.caching=false")
	})

	t.Run("When gradle properties file is missing throws error", func(t *testing.T) {
		noFileError := fmt.Errorf("there is no gradle properties file")
		updater := gradlePropertiesUpdater{
			func(string) (string, bool, error) {
				return "", false, noFileError
			},
			gradleconfig.DefaultOsProxy(),
		}

		mockLogger, tmpGradleHomeDir, propertyFilePath := prep()

		// when
		err := updater.updateGradleProps(
			ActivateForGradleParams{
				Cache: CacheParams{
					Enabled: true,
				},
			},
			mockLogger,
			tmpGradleHomeDir,
		)

		// then
		require.EqualError(t, err, fmt.Errorf(errFmtGradlePropertiesCheck, propertyFilePath, noFileError).Error())
	})

	t.Run("When failing to update gradle.properties throws error", func(t *testing.T) {
		failedToWriteError := fmt.Errorf("couldn't write gradle properties file")
		updater := gradlePropertiesUpdater{
			readFileIfExists,
			gradleconfig.OsProxy{
				WriteFile: func(string, []byte, os.FileMode) error {
					return failedToWriteError
				},
			},
		}

		mockLogger, tmpGradleHomeDir, propertyFilePath := prep()

		// when
		err := updater.updateGradleProps(
			ActivateForGradleParams{
				Cache: CacheParams{
					Enabled: true,
				},
			},
			mockLogger,
			tmpGradleHomeDir,
		)

		// then
		require.EqualError(t, err, fmt.Errorf(errFmtGradlePropertyWrite, propertyFilePath, failedToWriteError).Error())
	})
}
