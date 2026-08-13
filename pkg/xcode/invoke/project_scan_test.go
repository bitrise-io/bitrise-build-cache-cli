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

func Test_scanProjectAncestor_projectPath(t *testing.T) {
	proxy := utils.DefaultOsProxy{}

	t.Run("workspace in cwd wins", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "App.xcworkspace"))

		got := scanProjectAncestor(root, root, proxy, nil)
		assert.Equal(t, filepath.Join(root, "App.xcworkspace"), got.projectPath)
	})

	t.Run("project in cwd wins when no workspace", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "App.xcodeproj"))

		got := scanProjectAncestor(root, root, proxy, nil)
		assert.Equal(t, filepath.Join(root, "App.xcodeproj"), got.projectPath)
	})

	t.Run("nearest ancestor wins in monorepo", func(t *testing.T) {
		root := t.TempDir()
		app := filepath.Join(root, "App")
		other := filepath.Join(root, "Other")
		mustMkdir(t, filepath.Join(app, "App.xcworkspace"))
		mustMkdir(t, filepath.Join(other, "Other.xcworkspace"))
		sub := filepath.Join(app, "Subdir")
		mustMkdir(t, sub)

		got := scanProjectAncestor(sub, root, proxy, nil)
		assert.Equal(t, filepath.Join(app, "App.xcworkspace"), got.projectPath)
	})

	t.Run("workspace beats project at same level", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "App.xcworkspace"))
		mustMkdir(t, filepath.Join(root, "App.xcodeproj"))

		got := scanProjectAncestor(root, root, proxy, nil)
		assert.Equal(t, filepath.Join(root, "App.xcworkspace"), got.projectPath)
	})

	t.Run("nothing between cwd and repoRoot returns empty", func(t *testing.T) {
		root := t.TempDir()
		sub := filepath.Join(root, "src", "deep")
		mustMkdir(t, sub)

		got := scanProjectAncestor(sub, root, proxy, nil)
		assert.Empty(t, got.projectPath)
	})

	t.Run("lexicographically smallest workspace wins", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "Beta.xcworkspace"))
		mustMkdir(t, filepath.Join(root, "Alpha.xcworkspace"))

		got := scanProjectAncestor(root, root, proxy, nil)
		assert.Equal(t, filepath.Join(root, "Alpha.xcworkspace"), got.projectPath)
	})

	t.Run("cwd outside repoRoot returns empty without walking", func(t *testing.T) {
		repoRoot := t.TempDir()
		unrelated := t.TempDir()
		mustMkdir(t, filepath.Join(unrelated, "Stray.xcworkspace"))

		got := scanProjectAncestor(unrelated, repoRoot, proxy, nil)
		assert.Empty(t, got.projectPath)
	})
}

func Test_scanProjectAncestor_projectDir(t *testing.T) {
	proxy := utils.DefaultOsProxy{}

	t.Run("project directory ancestor of cwd", func(t *testing.T) {
		root := t.TempDir()
		app := filepath.Join(root, "App")
		mustMkdir(t, filepath.Join(app, "App.xcworkspace"))
		sub := filepath.Join(app, "subdir")
		mustMkdir(t, sub)

		got := scanProjectAncestor(sub, root, proxy, nil)
		assert.Equal(t, app, got.projectDir)
	})

	t.Run("monorepo sibling projects", func(t *testing.T) {
		root := t.TempDir()
		ios := filepath.Join(root, "apps", "ios")
		android := filepath.Join(root, "apps", "android")
		mustMkdir(t, filepath.Join(ios, "App.xcworkspace"))
		mustMkdir(t, filepath.Join(android, "AndroidApp.xcodeproj"))
		deep := filepath.Join(ios, "deep", "deeper")
		mustMkdir(t, deep)

		got := scanProjectAncestor(deep, root, proxy, nil)
		assert.Equal(t, ios, got.projectDir)
	})

	t.Run("cwd equals repoRoot with project at root", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "App.xcworkspace"))

		got := scanProjectAncestor(root, root, proxy, nil)
		assert.Equal(t, root, got.projectDir)
	})

	t.Run("no project between cwd and repoRoot yields empty projectDir", func(t *testing.T) {
		root := t.TempDir()
		sub := filepath.Join(root, "scripts")
		mustMkdir(t, sub)

		got := scanProjectAncestor(sub, root, proxy, nil)
		assert.Empty(t, got.projectDir)
	})
}
