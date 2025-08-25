// nolint: gochecknoglobals
package cmd_test

import (
	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
)

var mockLogger = &utilsMocks.Logger{}

func init() {
	mockLogger.On("TInfof").Return()
	mockLogger.On("TInfof", mock.Anything).Return()
	mockLogger.On("TInfof", mock.Anything, mock.Anything).Return()
	mockLogger.On("TInfof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TInfof", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof").Return()
	mockLogger.On("Infof", mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf").Return()
	mockLogger.On("Debugf", mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TErrorf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TDonef", mock.Anything).Return()
	mockLogger.On("TDonef", mock.Anything, mock.Anything).Return()
}
