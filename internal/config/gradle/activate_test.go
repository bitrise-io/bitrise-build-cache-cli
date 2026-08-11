package gradleconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func Test_Activate_CachePushImpliesCacheEnabled(t *testing.T) {
	mockLogger := &mocks.Logger{}
	mockLogger.On("Infof", mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()

	gradleHome := t.TempDir()

	var capturedInventory TemplateInventory
	templateWriter := func(inv TemplateInventory, _ string) error {
		capturedInventory = inv

		return nil
	}

	envs := map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
	}

	params := ActivateGradleParams{
		Cache: CacheParams{
			Enabled:         false,
			PushEnabled:     true,
			ValidationLevel: string(CacheValidationLevelWarning),
			Endpoint:        "EndpointValue",
		},
	}

	err := Activate(
		mockLogger,
		gradleHome,
		envs,
		false,
		params.TemplateInventory,
		templateWriter,
		GradlePropertiesUpdater{OsProxy: utils.DefaultOsProxy{}},
		params,
	)
	require.NoError(t, err)

	assert.Equal(t, UsageLevelEnabled, capturedInventory.Cache.Usage)
	assert.True(t, capturedInventory.Cache.IsPushEnabled)

	propsBytes, readErr := os.ReadFile(filepath.Join(gradleHome, "gradle.properties"))
	require.NoError(t, readErr)
	assert.Contains(t, string(propsBytes), "org.gradle.caching=true")
}

//nolint:dupl
func Test_Activate_BenchmarkBaselineDisablesCacheInProps(t *testing.T) {
	mockLogger := &mocks.Logger{}
	mockLogger.On("Infof", mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()

	gradleHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITRISE_BUILD_CACHE_BENCHMARK_PHASE_GRADLE", "baseline")

	var capturedInventory TemplateInventory
	templateWriter := func(inv TemplateInventory, _ string) error {
		capturedInventory = inv

		return nil
	}

	envs := map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		"BITRISE_IO":                       "true",
		"BITRISE_BUILD_SLUG":               "build-slug",
		"BITRISE_APP_SLUG":                 "app-slug",
		"BITRISE_TRIGGERED_WORKFLOW_ID":    "workflow",
	}

	params := ActivateGradleParams{
		Cache: CacheParams{
			Enabled:         true,
			PushEnabled:     true,
			ValidationLevel: string(CacheValidationLevelWarning),
			Endpoint:        "EndpointValue",
		},
	}

	err := Activate(
		mockLogger,
		gradleHome,
		envs,
		false,
		params.TemplateInventory,
		templateWriter,
		GradlePropertiesUpdater{OsProxy: utils.DefaultOsProxy{}},
		params,
	)
	require.NoError(t, err)

	assert.Equal(t, UsageLevelNone, capturedInventory.Cache.Usage)

	propsBytes, readErr := os.ReadFile(filepath.Join(gradleHome, "gradle.properties"))
	require.NoError(t, readErr)
	assert.Contains(t, string(propsBytes), "org.gradle.caching=false")
}

//nolint:dupl
func Test_Activate_BenchmarkWarmupKeepsCacheEnabledInProps(t *testing.T) {
	mockLogger := &mocks.Logger{}
	mockLogger.On("Infof", mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()

	gradleHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BITRISE_BUILD_CACHE_BENCHMARK_PHASE_GRADLE", "warmup")

	var capturedInventory TemplateInventory
	templateWriter := func(inv TemplateInventory, _ string) error {
		capturedInventory = inv

		return nil
	}

	envs := map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		"BITRISE_IO":                       "true",
		"BITRISE_BUILD_SLUG":               "build-slug",
		"BITRISE_APP_SLUG":                 "app-slug",
		"BITRISE_TRIGGERED_WORKFLOW_ID":    "workflow",
	}

	params := ActivateGradleParams{
		Cache: CacheParams{
			Enabled:         true,
			PushEnabled:     true,
			ValidationLevel: string(CacheValidationLevelWarning),
			Endpoint:        "EndpointValue",
		},
	}

	err := Activate(
		mockLogger,
		gradleHome,
		envs,
		false,
		params.TemplateInventory,
		templateWriter,
		GradlePropertiesUpdater{OsProxy: utils.DefaultOsProxy{}},
		params,
	)
	require.NoError(t, err)

	assert.Equal(t, UsageLevelEnabled, capturedInventory.Cache.Usage)

	propsBytes, readErr := os.ReadFile(filepath.Join(gradleHome, "gradle.properties"))
	require.NoError(t, readErr)
	assert.Contains(t, string(propsBytes), "org.gradle.caching=true")
}
