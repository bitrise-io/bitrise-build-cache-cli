//go:build unit

package xcode

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// The caller of this reads stdout and exports the result as an env var, so a
// stray log line or a missing newline breaks whatever consumes it downstream.
func TestDerivedDataPathCmd_PrintsTheResolvedRootAndNothingElse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	out := &bytes.Buffer{}
	derivedDataPathCmd.SetOut(out)
	derivedDataPathCmd.SetErr(&bytes.Buffer{})
	t.Cleanup(func() { derivedDataPathCmd.SetOut(nil); derivedDataPathCmd.SetErr(nil) })

	require.NoError(t, derivedDataPathCmd.RunE(derivedDataPathCmd, nil))

	// Same source of truth activation uses, not a second copy of the path.
	assert.Equal(t, paths.FromHome(home).XcodeManagedDerivedDataRoot()+"\n", out.String())
}
