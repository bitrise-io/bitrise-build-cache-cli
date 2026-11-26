// nolint: gochecknoglobals
package bazel_test

import (
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/testhome"
)

var mockLogger = &utilsMocks.Logger{}

func init() {
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything).Return()
	// The test binary itself lives on a transient path, so CLI-path resolution
	// logs where the generated config will look for the binary.
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	// clibin.logResolution logs a 3-vararg Infof when the running binary path
	// differs from the on-PATH copy (bare CLI name + on-PATH + running exe).
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything).Return()
}

func TestMain(m *testing.M) {
	testhome.Main(m)
}
