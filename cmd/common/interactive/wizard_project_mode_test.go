//go:build unit

package interactive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// stubConfirmMarker swaps the opt-in confirm step with a fake answer.
func stubConfirmMarker(t *testing.T, answer bool, err error) func() {
	t.Helper()

	prev := confirmMarker
	confirmMarker = func(string) (bool, error) { return answer, err }

	return func() { confirmMarker = prev }
}

func TestConfirmMarkerForCwd_YesWritesMarker(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	defer stubConfirmMarker(t, true, nil)()

	confirmMarkerForCwd(silentLogger(), utils.DefaultOsProxy{})

	body, err := os.ReadFile(filepath.Join(cwd, paths.ProjectMarkerFilename)) //nolint:gosec // tempdir
	require.NoError(t, err)
	assert.Equal(t, "{}\n", string(body))
}

func TestConfirmMarkerForCwd_NoLeavesDirUnopted(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	defer stubConfirmMarker(t, false, nil)()

	confirmMarkerForCwd(silentLogger(), utils.DefaultOsProxy{})

	_, err := os.Stat(filepath.Join(cwd, paths.ProjectMarkerFilename))
	assert.True(t, os.IsNotExist(err), "no on the confirm must not write a marker")
}

func TestConfirmMarkerForCwd_AncestorMarkerShortCircuits(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
	cwd := filepath.Join(root, "child")
	require.NoError(t, os.MkdirAll(cwd, 0o755))
	t.Chdir(cwd)

	called := 0
	prev := confirmMarker
	confirmMarker = func(string) (bool, error) {
		called++

		return true, nil
	}
	defer func() { confirmMarker = prev }()

	confirmMarkerForCwd(silentLogger(), utils.DefaultOsProxy{})

	assert.Zero(t, called, "a covered dir must short-circuit before the confirm form")
	_, err := os.Stat(filepath.Join(cwd, paths.ProjectMarkerFilename))
	assert.True(t, os.IsNotExist(err))
}

func TestConfirmMarkerForCwd_PromptErrorWarnsAndContinues(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	defer stubConfirmMarker(t, false, errors.New("boom"))()

	assert.NotPanics(t, func() {
		confirmMarkerForCwd(silentLogger(), utils.DefaultOsProxy{})
	})

	_, err := os.Stat(filepath.Join(cwd, paths.ProjectMarkerFilename))
	assert.True(t, os.IsNotExist(err), "a prompt error must not leave a marker behind")
}
