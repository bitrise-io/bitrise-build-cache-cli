//go:build unit

package daemon

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecRunner_pinsLocale exercises the real ExecRunner against /bin/sh
// to verify LC_ALL=C / LANG=C are actually exported into the supervisor
// child env. The production code matches systemd error strings as
// substrings and would silently break on a non-English shell — this is the
// regression guard.
func TestExecRunner_pinsLocale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ExecRunner targets POSIX supervisors only")
	}

	stdout, _, code, err := ExecRunner{}.Run(context.Background(), "/bin/sh", "-c", "echo LC_ALL=$LC_ALL LANG=$LANG")
	require.NoError(t, err)
	require.Equal(t, 0, code)

	out := strings.TrimSpace(stdout)
	assert.Contains(t, out, "LC_ALL=C")
	assert.Contains(t, out, "LANG=C")
}
