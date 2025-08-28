package gradleconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
)

func Test_gradlePropertiesFromParams(t *testing.T) {
	prep := func() (log.Logger, string, string) {
		mockLogger := &utilsMocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()

		tmpPath := t.TempDir()
		tmpGradleHomeDir := filepath.Join(tmpPath, ".gradle")
		_ = os.MkdirAll(tmpGradleHomeDir, 0o755)

		propertyFilePath := filepath.Join(tmpGradleHomeDir, "gradle.properties")

		return mockLogger, tmpGradleHomeDir, propertyFilePath
	}

	t.Run("Update gradle properties", func(t *testing.T) {
		updater := GradlePropertiesUpdater{utils.DefaultOsProxy{}}

		mockLogger, tmpGradleHomeDir, propertyFilePath := prep()

		// when
		err := updater.UpdateGradleProps(
			ActivateGradleParams{
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
		updater := GradlePropertiesUpdater{utils.DefaultOsProxy{}}

		mockLogger, tmpGradleHomeDir, propertyFilePath := prep()

		// when
		err := updater.UpdateGradleProps(
			ActivateGradleParams{
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
		osProxy := &mocks.OsProxyMock{
			ReadFileIfExistsFunc: func(_ string) (string, bool, error) {
				return "", false, noFileError
			},
		}

		updater := GradlePropertiesUpdater{osProxy}

		mockLogger, tmpGradleHomeDir, propertyFilePath := prep()

		// when
		err := updater.UpdateGradleProps(
			ActivateGradleParams{
				Cache: CacheParams{
					Enabled: true,
				},
			},
			mockLogger,
			tmpGradleHomeDir,
		)

		// then
		assert.EqualError(t, err, fmt.Errorf(ErrFmtGradlePropertiesCheck, propertyFilePath, noFileError).Error())
	})

	t.Run("When failing to update gradle.properties throws error", func(t *testing.T) {
		failedToWriteError := fmt.Errorf("couldn't write gradle properties file")

		osProxy := &mocks.OsProxyMock{
			ReadFileIfExistsFunc: func(_ string) (string, bool, error) {
				return "", true, nil
			},
			WriteFileFunc: func(_ string, _ []byte, _ os.FileMode) error {
				return failedToWriteError
			},
		}
		updater := GradlePropertiesUpdater{osProxy}

		mockLogger, tmpGradleHomeDir, propertyFilePath := prep()

		// when
		err := updater.UpdateGradleProps(
			ActivateGradleParams{
				Cache: CacheParams{
					Enabled: true,
				},
			},
			mockLogger,
			tmpGradleHomeDir,
		)

		// then
		assert.EqualError(t, err, fmt.Errorf(ErrFmtGradlePropertyWrite, propertyFilePath, failedToWriteError).Error())
	})
}
