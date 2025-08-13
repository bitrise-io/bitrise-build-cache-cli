package bazelconfig

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"text/template"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_WriteToBazelrc(t *testing.T) {
	logger := func() log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	t.Run("writes new bazelrc file when it doesn't exist", func(t *testing.T) {
		mockLogger := logger()
		inventory := TemplateInventory{
			Common: CommonTemplateInventory{
				AuthToken: "AuthTokenValue",
			},
			Cache: CacheTemplateInventory{
				Enabled: true,
			},
		}

		mockOsProxy := &utils.MockOsProxy{
			ReadFileIfExistsFunc: func(path string) (string, bool, error) {
				return "", true, nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				return nil
			},
		}

		err := inventory.WriteToBazelrc(mockLogger, "test/.bazelrc", mockOsProxy, utils.DefaultTemplateProxy())
		assert.NoError(t, err)

		// Verify written content
		require.Len(t, mockOsProxy.WriteFileCalls(), 1)
		writtenContent := mockOsProxy.WriteFileCalls()[0].Bytes
		assert.Contains(t, string(writtenContent), "# [start] generated-by-bitrise-build-cache")
		assert.Contains(t, string(writtenContent), "# [end] generated-by-bitrise-build-cache")
		assert.Contains(t, string(writtenContent), "--remote_header=authorization=\"Bearer AuthTokenValue\"")
	})

	t.Run("preserves existing content and updates block", func(t *testing.T) {
		mockLogger := logger()
		inventory := TemplateInventory{
			Common: CommonTemplateInventory{
				AuthToken: "NewAuthToken",
			},
			Cache: CacheTemplateInventory{
				Enabled: true,
			},
		}

		existingContent := `# Existing bazel config
build --cpu=x86_64

# [start] generated-by-bitrise-build-cache
build --remote_header=authorization="Bearer OldAuthToken"
# [end] generated-by-bitrise-build-cache

# Other settings
build --cpp_opt="-O2"`

		mockOsProxy := &utils.MockOsProxy{
			ReadFileIfExistsFunc: func(path string) (string, bool, error) {
				return existingContent, true, nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				return nil
			},
		}

		err := inventory.WriteToBazelrc(mockLogger, "test/.bazelrc", mockOsProxy, utils.DefaultTemplateProxy())
		assert.NoError(t, err)

		// Verify written content preserves original content
		require.Len(t, mockOsProxy.WriteFileCalls(), 1)
		writtenContent := mockOsProxy.WriteFileCalls()[0].Bytes

		assert.Contains(t, string(writtenContent), "# Existing bazel config")
		assert.Contains(t, string(writtenContent), "build --cpu=x86_64")
		assert.Contains(t, string(writtenContent), "# Other settings")
		assert.Contains(t, string(writtenContent), "build --cpp_opt=\"-O2\"")

		// Verify block is updated
		assert.Contains(t, string(writtenContent), "# [start] generated-by-bitrise-build-cache")
		assert.Contains(t, string(writtenContent), "# [end] generated-by-bitrise-build-cache")
		assert.Contains(t, string(writtenContent), "Bearer NewAuthToken")
		assert.NotContains(t, string(writtenContent), "Bearer OldAuthToken")
	})

	t.Run("when template parsing fails throws error", func(t *testing.T) {
		mockLogger := logger()
		mockOsProxy := &utils.MockOsProxy{
			ReadFileIfExistsFunc: func(path string) (string, bool, error) {
				return "", true, nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				return nil
			},
		}
		inventory := TemplateInventory{}
		expectedError := errors.New("failed to parse template")
		templateProxy := utils.TemplateProxy{
			Parse: func(string, string) (*template.Template, error) {
				return nil, expectedError
			},
			Execute: utils.DefaultTemplateProxy().Execute,
		}

		err := inventory.WriteToBazelrc(mockLogger, "test/.bazelrc", mockOsProxy, templateProxy)
		assert.ErrorContains(t, err, expectedError.Error())
	})

	t.Run("when template execution fails throws error", func(t *testing.T) {
		mockLogger := logger()
		mockOsProxy := &utils.MockOsProxy{
			ReadFileIfExistsFunc: func(path string) (string, bool, error) {
				return "", true, nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				return nil
			},
		}
		inventory := TemplateInventory{}
		expectedError := errors.New("failed to execute template")
		templateProxy := utils.TemplateProxy{
			Parse: utils.DefaultTemplateProxy().Parse,
			Execute: func(*template.Template, *bytes.Buffer, interface{}) error {
				return expectedError
			},
		}

		err := inventory.WriteToBazelrc(mockLogger, "test/.bazelrc", mockOsProxy, templateProxy)
		assert.ErrorContains(t, err, expectedError.Error())
	})

	t.Run("when reading bazelrc fails throws error", func(t *testing.T) {
		mockLogger := logger()
		inventory := TemplateInventory{}
		expectedError := errors.New("failed to read bazelrc")

		mockOsProxy := &utils.MockOsProxy{
			ReadFileIfExistsFunc: func(path string) (string, bool, error) {
				return "", false, expectedError
			},
		}

		err := inventory.WriteToBazelrc(mockLogger, "test/.bazelrc", mockOsProxy, utils.DefaultTemplateProxy())
		assert.ErrorContains(t, err, expectedError.Error())
	})

	t.Run("when writing bazelrc fails throws error", func(t *testing.T) {
		mockLogger := logger()
		inventory := TemplateInventory{}
		expectedError := errors.New("failed to write bazelrc")

		mockOsProxy := &utils.MockOsProxy{
			ReadFileIfExistsFunc: func(path string) (string, bool, error) {
				return "", true, nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				return expectedError
			},
		}

		err := inventory.WriteToBazelrc(mockLogger, "test/.bazelrc", mockOsProxy, utils.DefaultTemplateProxy())
		assert.ErrorContains(t, err, expectedError.Error())
	})
}
