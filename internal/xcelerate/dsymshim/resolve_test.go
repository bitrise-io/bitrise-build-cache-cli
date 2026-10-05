//go:build unit

package dsymshim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestResolve_EnvOverridesWinOverAnything(t *testing.T) {
	plugin, cas, note := Resolve(
		[]string{"/bin.o"},
		map[string]string{
			EnvPluginPathOverride: "/opt/libCAS.dylib",
			EnvCASPathOverride:    "/opt/cas",
		},
		utils.DefaultOsProxy{},
	)

	assert.Equal(t, "/opt/libCAS.dylib", plugin)
	assert.Equal(t, "/opt/cas", cas)
	assert.Contains(t, note, "plugin=env")
	assert.Contains(t, note, "cas=env")
}

func TestResolve_PluginFromDeveloperDirStatSucceeds(t *testing.T) {
	developerDir := t.TempDir()
	libDir := filepath.Join(developerDir, "usr", "lib")
	require.NoError(t, os.MkdirAll(libDir, 0o755))
	plugin := filepath.Join(libDir, "libToolchainCASPlugin.dylib")
	require.NoError(t, os.WriteFile(plugin, []byte("x"), 0o644))

	got, _, note := Resolve(
		[]string{"/bin.o"},
		map[string]string{"DEVELOPER_DIR": developerDir},
		utils.DefaultOsProxy{},
	)
	assert.Equal(t, plugin, got)
	assert.Contains(t, note, "plugin=developer-dir")
}

func TestResolve_CasPathWalkedFromMachO(t *testing.T) {
	dd := t.TempDir()
	plugin := filepath.Join(dd, "CompilationCache.noindex", "plugin")
	require.NoError(t, os.MkdirAll(plugin, 0o755))

	// Pretend the Mach-O lives several levels under DD.
	deep := filepath.Join(dd, "Build", "Intermediates.noindex", "App.build", "Release", "Binary.o")
	require.NoError(t, os.MkdirAll(filepath.Dir(deep), 0o755))
	require.NoError(t, os.WriteFile(deep, []byte("x"), 0o644))

	_, cas, note := Resolve(
		[]string{deep, "-o", "/x.dSYM"},
		map[string]string{},
		utils.DefaultOsProxy{},
	)
	assert.Equal(t, plugin, cas)
	assert.Contains(t, note, "cas=walked")
}
