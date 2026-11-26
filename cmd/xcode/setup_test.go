// nolint: gochecknoglobals
package xcode_test

import (
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/testhome"
)

var mockLogger = &utilsMocks.Logger{}

func init() {
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
	mockLogger.On("TInfof", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TInfof", mock.Anything, mock.Anything).Return()
	mockLogger.On("TWarnf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TWarnf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TWarnf", mock.Anything).Return()
	mockLogger.On("Println").Return()
	mockLogger.On("Printf", mock.Anything).Return()
	mockLogger.On("Printf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Printf", mock.Anything, mock.Anything, mock.Anything).Return()
}

func TestMain(m *testing.M) {
	testhome.Main(m)
}
