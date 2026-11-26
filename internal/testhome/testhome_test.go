//go:build unit

package testhome

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetup_MovesEveryMachineWideLocationAside(t *testing.T) {
	realTmp := os.TempDir()
	t.Setenv("HOME", "/home/real")
	t.Setenv("TMPDIR", realTmp)
	t.Setenv("ENVMAN_ENVSTORE_PATH", "/ci/envstore.yml")
	t.Setenv("GITHUB_ENV", "/ci/github_env")
	t.Setenv("BITRISE_XCELERATE_PROXY_SOCKET_PATH", "/tmp/xcelerate-proxy.sock")
	t.Setenv(envMarker, "")

	cleanup := Setup()
	home, tmp := os.Getenv("HOME"), os.Getenv("TMPDIR")

	assert.NotEqual(t, "/home/real", home)
	assert.NotEqual(t, realTmp, tmp)
	assert.NotEqual(t, home, tmp, "HOME and TMPDIR stay apart so a HOME sweep is meaningful")
	assert.Equal(t, filepath.Join(home, ".envstore.yml"), os.Getenv("ENVMAN_ENVSTORE_PATH"))
	assert.Empty(t, os.Getenv("GITHUB_ENV"))
	assert.Empty(t, os.Getenv("BITRISE_XCELERATE_PROXY_SOCKET_PATH"))

	cleanup()
	assert.NoDirExists(t, home)
	assert.NoDirExists(t, tmp)
}

func TestSetup_IgnoresAMarkerWhoseDirIsGone(t *testing.T) {
	t.Setenv("HOME", "/home/real")
	t.Setenv("TMPDIR", os.TempDir())
	t.Setenv(envMarker, filepath.Join(t.TempDir(), "gone"))

	t.Cleanup(Setup())

	assert.NotEqual(t, "/home/real", os.Getenv("HOME"))
}

// A parent may narrow HOME per test; the child must not widen it back to the marker.
func TestSetup_ChildKeepsTheHomeAndTempDirItWasGiven(t *testing.T) {
	t.Setenv(envMarker, t.TempDir())
	t.Setenv("HOME", "/per-test/home")
	t.Setenv("TMPDIR", "/parent/tmp")

	t.Cleanup(Setup())

	assert.Equal(t, "/per-test/home", os.Getenv("HOME"))
	assert.Equal(t, "/parent/tmp", os.Getenv("TMPDIR"))
}
