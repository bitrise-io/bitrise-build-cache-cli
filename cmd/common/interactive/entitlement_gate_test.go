//go:build unit

package interactive

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

// The wizard calls the config layer directly, so it needs its own gate.
func TestRunSelectedTools_WritesNothingForAWorkspaceWithoutBuildCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(configcommon.EnvSkipEntitlementCheck, "")
	configcommon.ResetEntitlementAnswers()
	t.Cleanup(configcommon.ResetEntitlementAnswers)
	asked := 0
	orig := configcommon.EntitlementChecker
	configcommon.EntitlementChecker = func(context.Context, string, auth.Credential, log.Logger) configcommon.EntitlementState {
		asked++

		return configcommon.EntitlementNone
	}
	t.Cleanup(func() { configcommon.EntitlementChecker = orig })
	envs := map[string]string{auth.EnvAuthToken: "a-token", auth.EnvWorkspaceID: "ws-1"}

	err := runSelectedTools(context.Background(), log.NewLogger(), []string{string(toolGradle), string(toolBazel)}, envs, false)

	require.NoError(t, err)
	assert.Equal(t, 1, asked)
	var files []string
	require.NoError(t, filepath.WalkDir(home, func(path string, _ fs.DirEntry, werr error) error {
		if werr == nil && path != home {
			files = append(files, path)
		}

		return werr
	}))
	assert.Empty(t, files)
}
