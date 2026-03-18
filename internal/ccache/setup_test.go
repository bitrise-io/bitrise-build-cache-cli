//go:build unit

// nolint: gochecknoglobals
package ccache

import (
	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
)

var mockLogger = &utilsMocks.Logger{}

func registerLoggerMethod(l *utilsMocks.Logger, method string) {
	l.On(method, mock.Anything).Return()
	l.On(method, mock.Anything, mock.Anything).Return()
	l.On(method, mock.Anything, mock.Anything, mock.Anything).Return()
	l.On(method, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
}

func init() {
	for _, method := range []string{"TDebugf", "TInfof", "TErrorf", "Warnf", "Infof", "Debugf"} {
		registerLoggerMethod(mockLogger, method)
	}
}
