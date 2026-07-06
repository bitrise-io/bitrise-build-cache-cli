//go:build unit

package mirrors_test

import (
	"os"
	"path/filepath"
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mirrorsconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle/mirrors"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func migrateTestLogger() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	l.On("Infof", mock.Anything).Return()
	l.On("Infof", mock.Anything, mock.Anything).Return()

	return l
}

func writePrebootMirror(t *testing.T, gradleHome string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(paths.GradleInitDir(gradleHome), 0o755))
	src := paths.GradleMirrorsInitScript(gradleHome)
	require.NoError(t, os.WriteFile(src, []byte("// preboot mirror"), 0o644))

	return src
}

func TestMigratePrebootInitScript_movesToCustomHome(t *testing.T) {
	defaultHome := filepath.Join(t.TempDir(), ".gradle")
	customHome := t.TempDir()
	src := writePrebootMirror(t, defaultHome)

	err := mirrorsconfig.MigratePrebootInitScript(migrateTestLogger(), utils.DefaultOsProxy{}, defaultHome, customHome)
	require.NoError(t, err)

	movedContent, err := os.ReadFile(paths.GradleMirrorsInitScript(customHome))
	require.NoError(t, err)
	require.Equal(t, "// preboot mirror", string(movedContent))

	_, statErr := os.Stat(src)
	require.True(t, os.IsNotExist(statErr), "preboot script should be removed after move")
}

func TestMigratePrebootInitScript_noopWhenHomesEqual(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".gradle")
	src := writePrebootMirror(t, home)

	err := mirrorsconfig.MigratePrebootInitScript(migrateTestLogger(), utils.DefaultOsProxy{}, home, home)
	require.NoError(t, err)

	_, statErr := os.Stat(src)
	require.NoError(t, statErr, "script must be left untouched when homes are equal")
}

func TestMigratePrebootInitScript_noopWhenNoPrebootScript(t *testing.T) {
	defaultHome := filepath.Join(t.TempDir(), ".gradle")
	customHome := t.TempDir()

	err := mirrorsconfig.MigratePrebootInitScript(migrateTestLogger(), utils.DefaultOsProxy{}, defaultHome, customHome)
	require.NoError(t, err)

	_, statErr := os.Stat(paths.GradleMirrorsInitScript(customHome))
	require.True(t, os.IsNotExist(statErr))
}

func TestMigratePrebootInitScript_doesNotClobberExistingTarget(t *testing.T) {
	defaultHome := filepath.Join(t.TempDir(), ".gradle")
	customHome := t.TempDir()
	src := writePrebootMirror(t, defaultHome)

	require.NoError(t, os.MkdirAll(paths.GradleInitDir(customHome), 0o755))
	dst := paths.GradleMirrorsInitScript(customHome)
	require.NoError(t, os.WriteFile(dst, []byte("// fresh mirror"), 0o644))

	err := mirrorsconfig.MigratePrebootInitScript(migrateTestLogger(), utils.DefaultOsProxy{}, defaultHome, customHome)
	require.NoError(t, err)

	dstContent, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "// fresh mirror", string(dstContent), "existing target must not be overwritten")

	_, statErr := os.Stat(src)
	require.NoError(t, statErr, "preboot script must be left in place when target already exists")
}
