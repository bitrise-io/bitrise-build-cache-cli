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

// TestOverrideFromExistingConfig_CarriesWrapperlessPipelineForward pins the
// re-run semantics: a user who ran `activate xcode --wrapperless-pipeline`
// once must keep the opt-in on a later `activate xcode` without the flag.
func TestOverrideFromExistingConfig_CarriesWrapperlessPipelineForward(t *testing.T) {
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

	params := Params{}
	overrideActivateXcodeParamsFromExistingConfig(
		log.NewLogger(), osProxy, &params, utils.DefaultDecoderFactory{}, map[string]string{})

	assert.True(t, params.WrapperlessPipelineEnabled, "re-run must preserve the persisted opt-in")
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
