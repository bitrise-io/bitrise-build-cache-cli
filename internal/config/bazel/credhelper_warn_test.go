//go:build unit

package bazelconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

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
	dir := gitRepoWithTrackedBazelrc(t,
		"build --credential_helper=*.services.bitrise.io=bitrise-build-cache\n")
	rc := filepath.Join(dir, ".bazelrc")

	mockLogger := &mocks.Logger{}
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()

	WarnIfHelperPinnedInRepo(mockLogger, dir, utils.DefaultOsProxy{})

	mockLogger.AssertCalled(t, "Warnf", "%s", mock.MatchedBy(func(s string) bool {
		return assert.Contains(t, s, rc) &&
			assert.Contains(t, s, "services.bitrise.io") &&
			assert.Contains(t, s, "installer.sh")
	}))
}

// gitRepoWithTrackedBazelrc creates a temp git repo with a committed .bazelrc,
// so ScanForPinnedHelper's default git-tracked filter sees the file.
func gitRepoWithTrackedBazelrc(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	rc := filepath.Join(dir, ".bazelrc")
	require.NoError(t, os.WriteFile(rc, []byte(body), 0o600))

	runGit(t, dir, "init", "-q")
	runGit(t, dir, "add", ".bazelrc")

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
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
