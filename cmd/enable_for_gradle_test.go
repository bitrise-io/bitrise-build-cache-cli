package cmd_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_enableForGradleCmdFn(t *testing.T) {
	tmpPath := t.TempDir()
	tmpGradleHomeDir := filepath.Join(tmpPath, ".gradle")

	t.Run("No envs specified", func(t *testing.T) {
		envVars := map[string]string{}
		err := cmd.EnableForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

		// then
		require.EqualError(t, err, fmt.Errorf(cmd.FmtErrorEnableForGradle, fmt.Errorf(gradleconfig.ErrFmtReadAutConfig, common.ErrAuthTokenNotProvided)).Error())
	})

	t.Run("RedactedEnvs specified", func(t *testing.T) {
		envVars := map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		}

		// when
		err := cmd.EnableForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

		// then
		require.NoError(t, err)
		//
		isInitFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache.init.gradle.kts"))
		require.NoError(t, err)
		assert.True(t, isInitFileExists)
		//
		isPropertiesFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "gradle.properties"))
		require.NoError(t, err)
		assert.True(t, isPropertiesFileExists)
	})
}
