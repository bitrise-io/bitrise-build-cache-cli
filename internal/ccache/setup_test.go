//go:build unit || integration

// nolint: gochecknoglobals
package ccache

import (
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/testhome"
)

var mockLogger = &utilsMocks.Logger{}

func registerLoggerMethod(l *utilsMocks.Logger, method string) {
	l.On(method, mock.Anything).Return()
	l.On(method, mock.Anything, mock.Anything).Return()
	l.On(method, mock.Anything, mock.Anything, mock.Anything).Return()
	l.On(method, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
}

func init() {
	for _, method := range []string{"TDebugf", "TInfof", "TErrorf", "TWarnf", "Warnf", "Infof", "Debugf"} {
		registerLoggerMethod(mockLogger, method)
	}
}

func TestMain(m *testing.M) {
	testhome.Main(m)
}
