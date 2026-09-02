//go:build unit

package gradleconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/stringmerge"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func newSilentGradleTestLogger() log.Logger {
	m := &mocks.Logger{}
	m.On("Infof", mock.Anything).Return()
	m.On("Infof", mock.Anything, mock.Anything).Return()
	m.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	m.On("Debugf", mock.Anything).Return()
	m.On("Debugf", mock.Anything, mock.Anything).Return()
	m.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	m.On("Debugf", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	m.On("Errorf", mock.Anything).Return()
	m.On("Errorf", mock.Anything, mock.Anything).Return()
	m.On("Warnf", mock.Anything).Return()
	m.On("Warnf", mock.Anything, mock.Anything).Return()
	m.On("TInfof", mock.Anything).Return()
	m.On("TInfof", mock.Anything, mock.Anything).Return()
	m.On("TInfof", mock.Anything, mock.Anything, mock.Anything).Return()
	m.On("TDonef", mock.Anything).Return()
	m.On("TDonef", mock.Anything, mock.Anything).Return()

	return m
}

func snapshotDir(t *testing.T, root string) map[string][]byte {
	t.Helper()

	out := map[string][]byte{}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		body, readErr := os.ReadFile(path) //nolint:gosec // test-only, walk-supplied path
		if readErr != nil {
			return readErr
		}
		out[rel] = body

		return nil
	}))

	return out
}

func TestDeactivate_Gradle_RoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	gradleHome := filepath.Join(tmpHome, ".gradle")
	require.NoError(t, os.MkdirAll(gradleHome, 0o755))

	// Pre-existing gradle.properties that must survive deactivate.
	preexisting := "user.name=alice\n"
	propsPath := filepath.Join(gradleHome, "gradle.properties")
	require.NoError(t, os.WriteFile(propsPath, []byte(preexisting), 0o644))

	pre := snapshotDir(t, tmpHome)

	logger := newSilentGradleTestLogger()

	params := ActivateGradleParams{
		Cache: CacheParams{
			Enabled:         true,
			PushEnabled:     true,
			ValidationLevel: string(CacheValidationLevelWarning),
			Endpoint:        "EndpointValue",
		},
	}

	envs := map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		"HOME":                             tmpHome,
	}

	require.NoError(t, Activate(
		logger,
		gradleHome,
		envs,
		false,
		params.TemplateInventory,
		func(inventory TemplateInventory, path string) error {
			return inventory.WriteToGradleInit(logger, path, utils.DefaultOsProxy{}, GradleTemplateProxy())
		},
		GradlePropertiesUpdater{OsProxy: utils.DefaultOsProxy{}},
		params,
	))

	// Sanity: activate created the init script + block.
	initScriptPath := paths.GradleInitScript(gradleHome)
	_, err := os.Stat(initScriptPath)
	require.NoError(t, err)

	propsAfterActivate, err := os.ReadFile(propsPath)
	require.NoError(t, err)
	assert.Contains(t, string(propsAfterActivate), "# [start] generated-by-bitrise-build-cache")

	// Also drop a sidecar so we cover its cleanup too.
	require.NoError(t, WriteSidecar(tmpHome, Sidecar{InitScriptPath: initScriptPath}))

	// Now deactivate.
	require.NoError(t, Deactivate(logger, DeactivateParams{
		GradleHome: gradleHome,
		Home:       tmpHome,
	}))

	// Init script gone.
	_, err = os.Stat(initScriptPath)
	assert.True(t, os.IsNotExist(err))

	// gradle.properties still exists, block gone, pre-existing content preserved.
	propsAfter, err := os.ReadFile(propsPath)
	require.NoError(t, err)
	assert.NotContains(t, string(propsAfter), "# [start] generated-by-bitrise-build-cache")
	assert.Contains(t, string(propsAfter), "user.name=alice")

	// Sidecar file gone.
	_, err = os.Stat(SidecarFilePath(tmpHome))
	assert.True(t, os.IsNotExist(err))

	// Snapshot check: only pre-activate files present.
	post := snapshotDir(t, tmpHome)
	assert.Equal(t, pre, post, "post-deactivate filesystem must match pre-activate snapshot")
}

func TestDeactivate_Gradle_Idempotent(t *testing.T) {
	tmpHome := t.TempDir()
	gradleHome := filepath.Join(tmpHome, ".gradle")

	logger := newSilentGradleTestLogger()

	require.NoError(t, Deactivate(logger, DeactivateParams{GradleHome: gradleHome, Home: tmpHome}))
	require.NoError(t, Deactivate(logger, DeactivateParams{GradleHome: gradleHome, Home: tmpHome}))
}

func TestDeactivate_Gradle_DryRunPreservesFiles(t *testing.T) {
	tmpHome := t.TempDir()
	gradleHome := filepath.Join(tmpHome, ".gradle")
	require.NoError(t, os.MkdirAll(filepath.Join(gradleHome, "init.d"), 0o755))
	initScript := paths.GradleInitScript(gradleHome)
	require.NoError(t, os.WriteFile(initScript, []byte("// generated"), 0o644))

	propsPath := filepath.Join(gradleHome, "gradle.properties")
	props := stringmerge.ChangeContentInBlock("user=alice\n", gradleBlockStart, gradleBlockEnd, "org.gradle.caching=true")
	require.NoError(t, os.WriteFile(propsPath, []byte(props), 0o644))

	logger := newSilentGradleTestLogger()
	require.NoError(t, Deactivate(logger, DeactivateParams{
		GradleHome: gradleHome,
		Home:       tmpHome,
		DryRun:     true,
	}))

	// Nothing changed.
	_, err := os.Stat(initScript)
	assert.NoError(t, err)

	after, err := os.ReadFile(propsPath)
	require.NoError(t, err)
	assert.Equal(t, props, string(after))
}
