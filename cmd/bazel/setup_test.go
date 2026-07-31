// nolint: gochecknoglobals
package bazel_test

import (
	"os"
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
)

var mockLogger = &utilsMocks.Logger{}

func init() {
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything).Return()
	// The test binary itself lives on a transient path, so CLI-path resolution
	// logs where the generated config will look for the binary.
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything).Return()
}

// TestMain points HOME at a throwaway dir. Several paths under test resolve
// their location from the home dir, so without this the suite writes into the
// developer's real home.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "bbc-bazel-test-home")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}

	code := m.Run()

	_ = os.RemoveAll(home)
	os.Exit(code)
}
