// nolint: gochecknoglobals
package xcelerate_test

import (
	"os"
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
)

var mockLogger = &utilsMocks.Logger{}

func init() {
	mockLogger.On("TInfof").Return()
	mockLogger.On("TInfof", mock.Anything).Return()
	mockLogger.On("TInfof", mock.Anything, mock.Anything).Return()
	mockLogger.On("TInfof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof").Return()
	mockLogger.On("Infof", mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf").Return()
	mockLogger.On("Debugf", mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf").Return()
	mockLogger.On("Warnf", mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TDebugf").Return()
	mockLogger.On("TDebugf", mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TDonef", mock.Anything).Return()
	mockLogger.On("TDonef", mock.Anything, mock.Anything).Return()
}

// TestMain points HOME at a throwaway dir. Several paths under test resolve
// their location from the home dir, so without this the suite writes into the
// developer's real home.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "bbc-xcelerate-test-home")
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
