//go:build unit

package invoke

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o755))
}

func Test_scanProjectFromCwd(t *testing.T) {
	proxy := utils.DefaultOsProxy{}

	t.Run("workspace in cwd wins", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "App.xcworkspace"))

		got, err := scanProjectFromCwd(root, root, proxy)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "App.xcworkspace"), got)
	})

	t.Run("project in cwd wins when no workspace", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "App.xcodeproj"))

		got, err := scanProjectFromCwd(root, root, proxy)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "App.xcodeproj"), got)
	})

	t.Run("nearest ancestor wins in monorepo", func(t *testing.T) {
		root := t.TempDir()
		app := filepath.Join(root, "App")
		other := filepath.Join(root, "Other")
		mustMkdir(t, filepath.Join(app, "App.xcworkspace"))
		mustMkdir(t, filepath.Join(other, "Other.xcworkspace"))
		sub := filepath.Join(app, "Subdir")
		mustMkdir(t, sub)

		got, err := scanProjectFromCwd(sub, root, proxy)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(app, "App.xcworkspace"), got)
	})

	t.Run("workspace beats project at same level", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "App.xcworkspace"))
		mustMkdir(t, filepath.Join(root, "App.xcodeproj"))

		got, err := scanProjectFromCwd(root, root, proxy)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "App.xcworkspace"), got)
	})

	t.Run("nothing between cwd and repoRoot returns empty", func(t *testing.T) {
		root := t.TempDir()
		sub := filepath.Join(root, "src", "deep")
		mustMkdir(t, sub)

		got, err := scanProjectFromCwd(sub, root, proxy)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("lexicographically smallest workspace wins", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "Beta.xcworkspace"))
		mustMkdir(t, filepath.Join(root, "Alpha.xcworkspace"))

		got, err := scanProjectFromCwd(root, root, proxy)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "Alpha.xcworkspace"), got)
	})

	t.Run("cwd outside repoRoot returns empty without walking", func(t *testing.T) {
		repoRoot := t.TempDir()
		unrelated := t.TempDir()
		mustMkdir(t, filepath.Join(unrelated, "Stray.xcworkspace"))

		got, err := scanProjectFromCwd(unrelated, repoRoot, proxy)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
