package utils_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
)

func TestActivateXcode_AddContentOrCreateFile(t *testing.T) {
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

		err := utils.AddContentOrCreateFile(
			mockLogger,
			osProxy,
			"test.txt",
			"Bitrise Xcelerate",
			"content",
		)

		require.NoError(t, err)
		require.Len(t, osProxy.ReadFileIfExistsCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.ReadFileIfExistsCalls()[0].Name)
		require.Len(t, osProxy.WriteFileCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.WriteFileCalls()[0].Name)
		assert.Equal(t, "# [start] Bitrise Xcelerate\ncontent\n# [end] Bitrise Xcelerate\n", string(osProxy.WriteFileCalls()[0].Data))
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

		err := utils.AddContentOrCreateFile(
			mockLogger,
			osProxy,
			"test.txt",
			"Bitrise Xcelerate",
			"content",
		)

		require.NoError(t, err)
		require.Len(t, osProxy.ReadFileIfExistsCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.ReadFileIfExistsCalls()[0].Name)
		require.Len(t, osProxy.WriteFileCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.WriteFileCalls()[0].Name)
		assert.Equal(t, "# [start] Bitrise Xcelerate\ncontent\n# [end] Bitrise Xcelerate\n", string(osProxy.WriteFileCalls()[0].Data))
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

		err := utils.AddContentOrCreateFile(
			mockLogger,
			osProxy,
			"test.txt",
			"# Bitrise Xcelerate",
			"content",
		)

		require.ErrorIs(t, err, expectedError)
		require.Len(t, osProxy.ReadFileIfExistsCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.ReadFileIfExistsCalls()[0].Name)
		require.Len(t, osProxy.WriteFileCalls(), 1)
		assert.Equal(t, "test.txt", osProxy.WriteFileCalls()[0].Name)
	})
}
