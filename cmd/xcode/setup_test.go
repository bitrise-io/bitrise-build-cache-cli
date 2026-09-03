// nolint: gochecknoglobals
package xcode_test

import (
	"os"
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
	keyring "github.com/zalando/go-keyring"
)

var mockLogger = &utilsMocks.Logger{}

func init() {
	keyring.MockInit()

	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything).Return()
	mockLogger.On("TDonef", mock.Anything, mock.Anything).Return()
	mockLogger.On("TErrorf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TErrorf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TInfof", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TInfof", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TInfof", mock.Anything, mock.Anything).Return()
	mockLogger.On("TWarnf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TWarnf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TWarnf", mock.Anything).Return()
}

// TestMain points HOME at a throwaway dir. Several paths under test resolve
// their location from the home dir, so without this the suite writes into the
// developer's real home.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "bbc-test-home")
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
