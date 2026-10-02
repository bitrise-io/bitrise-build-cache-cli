//go:build unit

package common_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrintOptInGateHintIfGated_WiredIntoActivateWrappers guards against a
// wrapper quietly losing the opt-in hint. The behaviour of the helper itself
// is covered by TestPrintOptInGateHintIfGated — this one only checks the five
// activate wrappers reference it.
func TestPrintOptInGateHintIfGated_WiredIntoActivateWrappers(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must succeed to resolve cmd/ root")
	cmdDir := filepath.Dir(filepath.Dir(thisFile))

	wrappers := []string{
		"xcode/activate_xcode.go",
		"gradle/activate_gradle.go",
		"ccache/activate_cpp.go",
		"bazel/activate_bazel.go",
		"reactnative/activate_react_native.go",
	}

	for _, rel := range wrappers {
		t.Run(rel, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(cmdDir, rel)) //nolint:gosec // test reads a sibling source file
			require.NoError(t, err)
			assert.True(t,
				strings.Contains(string(body), "common.PrintOptInGateHintIfGated("),
				"activate wrapper %s must call common.PrintOptInGateHintIfGated so opt-in gating doesn't silently drop cache", rel)
		})
	}
}
