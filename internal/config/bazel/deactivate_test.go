//go:build unit

package bazelconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/stringmerge"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func newSilentBazelTestLogger() log.Logger {
	m := &mocks.Logger{}
	m.On("Infof", mock.Anything).Return()
	m.On("Infof", mock.Anything, mock.Anything).Return()
	m.On("Debugf", mock.Anything).Return()
	m.On("Debugf", mock.Anything, mock.Anything).Return()
	m.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	m.On("TInfof", mock.Anything).Return()
	m.On("TInfof", mock.Anything, mock.Anything).Return()
	m.On("TInfof", mock.Anything, mock.Anything, mock.Anything).Return()

	return m
}

func TestDeactivate_Bazel_RoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	bazelrcPath := filepath.Join(tmpHome, ".bazelrc")

	preexisting := "build --disk_cache=/tmp/local\n"
	require.NoError(t, os.WriteFile(bazelrcPath, []byte(preexisting), 0o644))

	logger := newSilentBazelTestLogger()

	// Fake activation: use TemplateInventory writer directly.
	inv := TemplateInventory{
		Common: CommonTemplateInventory{AuthToken: "T", WorkspaceID: "W"},
		Cache:  CacheTemplateInventory{Enabled: true, EndpointURLWithPort: "grpcs://x:443"},
	}
	require.NoError(t, inv.WriteToBazelrc(logger, bazelrcPath, utils.DefaultOsProxy{}, utils.DefaultTemplateProxy()))

	// Also drop a sidecar under ~/.bitrise/cache/bazel.
	require.NoError(t, WriteSidecar(tmpHome, Sidecar{BazelrcPath: bazelrcPath, CacheEnabled: true}))

	activatedContent, err := os.ReadFile(bazelrcPath)
	require.NoError(t, err)
	assert.Contains(t, string(activatedContent), "# [start] generated-by-bitrise-build-cache")

	// Now deactivate.
	require.NoError(t, Deactivate(logger, DeactivateParams{
		BazelrcPath: bazelrcPath,
		Home:        tmpHome,
	}))

	after, err := os.ReadFile(bazelrcPath)
	require.NoError(t, err)
	assert.Equal(t, preexisting, string(after))

	_, err = os.Stat(SidecarFilePath(tmpHome))
	assert.True(t, os.IsNotExist(err))
}

func TestDeactivate_Bazel_Idempotent(t *testing.T) {
	tmpHome := t.TempDir()
	bazelrcPath := filepath.Join(tmpHome, ".bazelrc")
	logger := newSilentBazelTestLogger()

	require.NoError(t, Deactivate(logger, DeactivateParams{BazelrcPath: bazelrcPath, Home: tmpHome}))
	require.NoError(t, Deactivate(logger, DeactivateParams{BazelrcPath: bazelrcPath, Home: tmpHome}))
}

func TestDeactivate_Bazel_DryRunPreservesFile(t *testing.T) {
	tmpHome := t.TempDir()
	bazelrcPath := filepath.Join(tmpHome, ".bazelrc")

	content := stringmerge.ChangeContentInBlock("keep=me\n", bazelBlockStart, bazelBlockEnd, "build --remote_cache=grpc://x")
	require.NoError(t, os.WriteFile(bazelrcPath, []byte(content), 0o644))

	logger := newSilentBazelTestLogger()
	require.NoError(t, Deactivate(logger, DeactivateParams{
		BazelrcPath: bazelrcPath,
		Home:        tmpHome,
		DryRun:      true,
	}))

	after, err := os.ReadFile(bazelrcPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(after))
}
