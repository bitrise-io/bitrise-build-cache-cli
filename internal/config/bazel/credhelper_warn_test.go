//go:build unit

package bazelconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestWarnIfHelperPinnedInRepo_silentWhenNoMatch(t *testing.T) {
	dir := t.TempDir()

	mockLogger := &mocks.Logger{}
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()

	WarnIfHelperPinnedInRepo(mockLogger, dir, utils.DefaultOsProxy{})

	mockLogger.AssertNotCalled(t, "Warnf", mock.Anything, mock.Anything)
	mockLogger.AssertNotCalled(t, "Warnf", mock.Anything)
}

func TestWarnIfHelperPinnedInRepo_warnsOnMatch(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".bazelrc")
	assert.NoError(t, os.WriteFile(rc,
		[]byte("build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n"), 0o600))

	mockLogger := &mocks.Logger{}
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()

	WarnIfHelperPinnedInRepo(mockLogger, dir, utils.DefaultOsProxy{})

	mockLogger.AssertCalled(t, "Warnf", "%s", mock.MatchedBy(func(s string) bool {
		return assert.Contains(t, s, rc) &&
			assert.Contains(t, s, "services.bitrise.io") &&
			assert.Contains(t, s, "installer.sh")
	}))
}

func TestPinnedHelperWarning_containsActionableText(t *testing.T) {
	msg := PinnedHelperWarning([]PinnedHelperMatch{
		{Path: "/repo/.bazelrc", LineNumber: 7, Line: "build --credential_helper=*.services.bitrise.io=bitrise-build-cache"},
	})

	assert.Contains(t, msg, "/repo/.bazelrc:7")
	assert.Contains(t, msg, "PATH")
	assert.Contains(t, msg, "installer.sh")
	assert.Contains(t, msg, "activate bazel")
}
