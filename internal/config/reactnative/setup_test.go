//go:build unit

//nolint:gochecknoglobals
package reactnative_test

import (
	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
)

var mockLogger = func() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	l.On("TInfof", mock.Anything).Return()
	l.On("TInfof", mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything, mock.Anything, mock.Anything).Return()
	l.On("Infof", mock.Anything).Return()
	l.On("Infof", mock.Anything, mock.Anything).Return()
	l.On("Debugf", mock.Anything).Return()
	l.On("Debugf", mock.Anything, mock.Anything).Return()
	l.On("Warnf", mock.Anything).Return()
	l.On("Warnf", mock.Anything, mock.Anything).Return()
	l.On("Errorf", mock.Anything).Return()
	l.On("Errorf", mock.Anything, mock.Anything).Return()

	return l
}()
