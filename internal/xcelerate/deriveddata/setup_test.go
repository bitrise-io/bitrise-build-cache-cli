package deriveddata

import (
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/testhome"
)

func setupTests() log.Logger {
	mockLogger := &mocks.Logger{}
	mockLogger.On("Infof", mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()

	return mockLogger
}

func TestMain(m *testing.M) {
	testhome.Main(m)
}
