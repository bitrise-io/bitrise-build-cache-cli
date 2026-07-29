package bazelconfig

import (
	"os"
	"testing"
)

// TestMain points HOME at a throwaway dir. Several paths under test resolve
// their location from the home dir, so without this the suite writes into the
// developer's real home.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "bbc-bazelconfig-test-home")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}

	code := m.Run()

	_ = os.RemoveAll(home)
	os.Exit(code)
}
