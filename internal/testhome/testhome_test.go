//go:build unit

package testhome

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetup_IgnoresAMarkerWhoseDirIsGone(t *testing.T) {
	t.Setenv("HOME", "/home/real")
	t.Setenv(envMarker, filepath.Join(t.TempDir(), "gone"))

	t.Cleanup(Setup())

	assert.NotEqual(t, "/home/real", os.Getenv("HOME"))
}

// A parent may narrow HOME per test; the child must not widen it back to the marker.
func TestSetup_ChildKeepsTheHomeItWasGiven(t *testing.T) {
	t.Setenv("HOME", "/per-test/home")
	t.Setenv(envMarker, t.TempDir())

	t.Cleanup(Setup())

	assert.Equal(t, "/per-test/home", os.Getenv("HOME"))
}
