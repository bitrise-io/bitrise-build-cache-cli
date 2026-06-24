//go:build unit

package exec

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecRunner_capturesStdoutStderrAndExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell required")
	}

	runner := ExecRunner{}

	stdout, stderr, code, err := runner.Run(context.Background(), "/bin/sh", "-c", "echo hi; echo err 1>&2; exit 7")
	require.NoError(t, err)
	assert.Equal(t, "hi\n", stdout)
	assert.Equal(t, "err\n", stderr)
	assert.Equal(t, 7, code)
}

func TestExecRunner_zeroExitReturnsCleanly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell required")
	}

	stdout, _, code, err := ExecRunner{}.Run(context.Background(), "/bin/sh", "-c", "echo ok")
	require.NoError(t, err)
	assert.Equal(t, "ok\n", stdout)
	assert.Equal(t, 0, code)
}

func TestExecRunner_pinLocaleAddsLCAllAndLang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell required")
	}

	stdout, _, code, err := ExecRunner{PinLocale: true}.Run(context.Background(), "/bin/sh", "-c", `echo "LC=${LC_ALL} LANG=${LANG}"`)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "LC=C")
	assert.Contains(t, stdout, "LANG=C")
}

func TestExecRunner_extraEnvForwarded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell required")
	}

	stdout, _, code, err := ExecRunner{Env: []string{"FOO=bar"}}.Run(context.Background(), "/bin/sh", "-c", `echo "FOO=$FOO"`)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "FOO=bar")
}

func TestExecRunner_missingBinaryReturnsErr(t *testing.T) {
	_, _, code, err := ExecRunner{}.Run(context.Background(), "/no/such/binary/here-bitrise-test", "--noop")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "run"), "err should mention run prefix: %v", err)
	assert.Equal(t, -1, code)
}
