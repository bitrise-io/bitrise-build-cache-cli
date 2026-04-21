//go:build unit

//nolint:gochecknoglobals
package status_test

import (
	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
)

var mockLogger = func() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	for _, name := range []string{"TInfof", "Infof", "Debugf", "TDebugf", "Warnf", "TWarnf", "Errorf", "TErrorf", "TDonef"} {
		l.On(name, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything).Return()
		l.On(name, mock.Anything, mock.Anything, mock.Anything).Return()
	}

	return l
}()
