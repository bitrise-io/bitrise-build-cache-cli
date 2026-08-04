//go:build unit

package doctor

import (
	"os"
	"testing"
)

// TestMain points HOME at a throwaway dir. Several checks read the activated
// tools' configs from the home dir, so without this the results depend on
// whether the developer running the suite has activated anything.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "bbc-doctor-test-home")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(home)

	os.Setenv("HOME", home)

	os.Exit(m.Run())
}
