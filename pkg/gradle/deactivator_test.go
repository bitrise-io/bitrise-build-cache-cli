//go:build unit

package gradle_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gradlepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/gradle"
)

func TestDeactivator_DryRun_LeavesFilesUntouched(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("GRADLE_USER_HOME", "")

	seed := map[string]string{
		filepath.Join(tmpHome, ".gradle", "gradle.properties"): "# [start] generated-by-bitrise-build-cache\norg.gradle.caching=true\n# [end] generated-by-bitrise-build-cache\n",
	}
	for path, content := range seed {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	d := gradlepkg.NewDeactivator(gradlepkg.DeactivatorParams{
		DryRun: true,
		Logger: log.NewLogger(),
	})
	require.NoError(t, d.Deactivate(context.Background()))

	for path, want := range seed {
		got, err := os.ReadFile(path) //nolint:gosec // seeded tmp path
		require.NoError(t, err)
		assert.Equal(t, want, string(got), "dry-run must not mutate %s", path)
	}
}
