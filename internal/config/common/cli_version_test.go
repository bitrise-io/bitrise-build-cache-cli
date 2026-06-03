//go:build unit

package common

import (
	"io"
	"os"
	"sync"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetVersionLogOnce(t *testing.T) {
	t.Helper()
	versionLogOnce = sync.Once{}
}

func TestGetCLIVersion_PrefersInjectedVersion(t *testing.T) {
	prev := injectedVersion
	t.Cleanup(func() { injectedVersion = prev })

	injectedVersion = "v9.9.9"
	assert.Equal(t, "v9.9.9", GetCLIVersion(log.NewLogger()))
}

func TestGetCLIVersion_TrimsInjectedVersion(t *testing.T) {
	prev := injectedVersion
	t.Cleanup(func() { injectedVersion = prev })

	injectedVersion = "  v1.2.3  "
	assert.Equal(t, "v1.2.3", GetCLIVersion(log.NewLogger()))
}

func TestGetCLIVersion_FallsBackWhenInjectedEmpty(t *testing.T) {
	prev := injectedVersion
	t.Cleanup(func() { injectedVersion = prev })

	injectedVersion = ""
	// Without ldflags injection we should resolve via debug.BuildInfo or
	// land on the "devel" sentinel — never an empty string.
	assert.NotEmpty(t, GetCLIVersion(log.NewLogger()))
}

func TestLogCLIVersion_PrintsAtMostOncePerProcess(t *testing.T) {
	prev := injectedVersion
	t.Cleanup(func() { injectedVersion = prev })

	resetVersionLogOnce(t)
	injectedVersion = "v1.2.3"

	stderr := captureStderr(t, func() {
		for i := 0; i < 5; i++ {
			LogCLIVersion(log.NewLogger())
		}
	})

	assert.Equal(t, "Bitrise Build Cache CLI version: v1.2.3\n", stderr)
}

// captureStderr swaps os.Stderr for a pipe, runs fn, restores the original
// stderr, and returns whatever fn wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(r)
		done <- string(buf)
	}()

	defer func() {
		os.Stderr = origStderr
	}()

	fn()

	require.NoError(t, w.Close())

	return <-done
}
