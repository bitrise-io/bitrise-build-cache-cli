//go:build unit

//nolint:gochecknoglobals
package status_test

import (
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/testhome"
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

func TestMain(m *testing.M) {
	testhome.Main(m)
}
