// nolint: gochecknoglobals
package proxy_test

import (
	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
)

var mockLogger = &utilsMocks.Logger{}

func init() {
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("TDebugf", mock.Anything).Return()
	mockLogger.On("TErrorf", mock.Anything, mock.Anything).Return()
}
