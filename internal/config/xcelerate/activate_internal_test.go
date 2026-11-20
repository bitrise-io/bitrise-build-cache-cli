//go:build unit

package xcelerate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestOverrideFromExistingConfig_CarriesWrapperlessPipelineForward(t *testing.T) {
	params := loadParamsWithSavedWrapperless(t, Params{})

	assert.True(t, params.WrapperlessPipelineEnabled, "silent re-run must preserve the persisted opt-in")
}

func TestOverrideFromExistingConfig_ExplicitFalseTurnsWrapperlessOff(t *testing.T) {
	params := loadParamsWithSavedWrapperless(t, Params{WrapperlessPipelineExplicit: true})

	assert.False(t, params.WrapperlessPipelineEnabled, "explicit --wrapperless-pipeline=false must beat persisted true")
}

func TestOverrideFromExistingConfig_ExplicitTrueStaysOn(t *testing.T) {
	params := loadParamsWithSavedWrapperless(t, Params{
		WrapperlessPipelineEnabled:  true,
		WrapperlessPipelineExplicit: true,
	})

	assert.True(t, params.WrapperlessPipelineEnabled, "explicit --wrapperless-pipeline=true must stay on")
}

func loadParamsWithSavedWrapperless(t *testing.T, params Params) Params {
	t.Helper()

	home := t.TempDir()
	osProxy := &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		OpenFileFunc:    os.OpenFile,
	}

	configPath := PathFor(osProxy, "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	body, err := json.Marshal(Config{WrapperlessPipelineEnabled: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, body, 0o600))

	overrideActivateXcodeParamsFromExistingConfig(
		log.NewLogger(), osProxy, &params, utils.DefaultDecoderFactory{}, map[string]string{})

	return params
}

func TestEnsureLogDir_CreatesWhatTheProxyWouldCreateLazily(t *testing.T) {
	home := t.TempDir()
	logDir := paths.FromHome(home).XcelerateLogDir()
	require.NoDirExists(t, logDir)

	ensureLogDir(log.NewLogger(), &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		MkdirAllFunc:    os.MkdirAll,
	})

	assert.DirExists(t, logDir)
}

func TestEnsureLogDir_FailureDoesNotStopActivation(t *testing.T) {
	ensureLogDir(log.NewLogger(), &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return "", errors.New("no home") },
		MkdirAllFunc:    func(string, os.FileMode) error { return errors.New("read-only fs") },
	})
}
