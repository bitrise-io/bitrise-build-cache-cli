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

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// useTempCredentialStore keeps the credential Activate pins off the real keychain.
func useTempCredentialStore(t *testing.T) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	original := newResolver
	newResolver = func(logger log.Logger) *live.Resolver {
		resolver := live.Default(logger)
		resolver.Backends = []store.Store{store.NewFile()}

		return resolver
	}
	t.Cleanup(func() { newResolver = original })
}

func Test_Activate_CachePushImpliesCacheEnabled(t *testing.T) {
	useTempCredentialStore(t)

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
		t.Context(),
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
	useTempCredentialStore(t)

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
		t.Context(),
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
	useTempCredentialStore(t)

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
		t.Context(),
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

func Test_Activate_PinsResolvedCredential(t *testing.T) {
	useTempCredentialStore(t)

	mockLogger := &mocks.Logger{}
	mockLogger.On("Infof", mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
	mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything).Return()
	mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything).Return()
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()

	envs := map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
	}

	params := ActivateGradleParams{
		Cache: CacheParams{Enabled: true, ValidationLevel: string(CacheValidationLevelWarning)},
	}

	err := Activate(
		t.Context(),
		mockLogger,
		t.TempDir(),
		envs,
		false,
		params.TemplateInventory,
		func(TemplateInventory, string) error { return nil },
		GradlePropertiesUpdater{OsProxy: utils.DefaultOsProxy{}},
		params,
	)
	require.NoError(t, err)

	// Without this the plugins have nothing to resolve once the env vars above are gone.
	pinned, loadErr := store.NewFile().Load()
	require.NoError(t, loadErr)
	assert.Equal(t, "AuthTokenValue", pinned.AuthToken)
	assert.Equal(t, "WorkspaceIDValue", pinned.WorkspaceID)
}
