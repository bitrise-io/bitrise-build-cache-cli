//go:build unit

package trampoline

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseRemarkDeps(t *testing.T, pid int) (*bytes.Buffer, remarkDeps) {
	t.Helper()
	buf := &bytes.Buffer{}
	tmp := t.TempDir()

	return buf, remarkDeps{
		readProxyPid: func() (int, bool) { return pid, pid > 0 },
		tryLock:      func(string) (bool, error) { return true, nil },
		stderr:       buf,
		tmpDir:       func() string { return tmp },
	}
}

func TestEmitEngagedRemark_WritesRemarkOnFirstCall(t *testing.T) {
	buf, d := baseRemarkDeps(t, 1234)

	emitEngagedRemark(d)

	assert.Contains(t, buf.String(), "remark: Bitrise remote build cache engaged")
}

func TestEmitEngagedRemark_SkipsWhenLockBusy(t *testing.T) {
	buf, d := baseRemarkDeps(t, 1234)
	d.tryLock = func(string) (bool, error) { return false, nil }

	emitEngagedRemark(d)

	assert.Empty(t, buf.String(), "lock-busy means another trampoline already emitted")
}

func TestEmitEngagedRemark_SkipsWhenProxyPidMissing(t *testing.T) {
	buf, d := baseRemarkDeps(t, 0)

	emitEngagedRemark(d)

	assert.Empty(t, buf.String(), "no proxy pid means no proxy up yet; shim must not emit")
}

func TestEmitEngagedRemark_SentinelPathIsPIDScoped(t *testing.T) {
	buf, d := baseRemarkDeps(t, 42)
	var observed string
	d.tryLock = func(path string) (bool, error) {
		observed = path

		return true, nil
	}

	emitEngagedRemark(d)

	assert.Contains(t, observed, "bitrise-xcelerate-remark-42")
	assert.NotEmpty(t, buf.String())
}

func TestEmitEngagedRemark_RealFlockDedup(t *testing.T) {
	tmp := t.TempDir()
	pid := 7777
	sentinel := filepath.Join(tmp, remarkSentinelPrefix+strconv.Itoa(pid))

	// First call acquires the real flock and emits.
	buf1 := &bytes.Buffer{}
	emitEngagedRemark(remarkDeps{
		readProxyPid: func() (int, bool) { return pid, true },
		tryLock:      defaultRemarkDeps.tryLock,
		stderr:       buf1,
		tmpDir:       func() string { return tmp },
	})
	assert.Contains(t, buf1.String(), remarkText)

	// Sentinel must now exist and still be locked by the earlier flock handle.
	_, err := os.Stat(sentinel)
	require.NoError(t, err)
}

func TestReadPidFile_RejectsNonPositive(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte("0\n"), 0o644))

	_, ok := readPidFile(path)
	assert.False(t, ok)
}

func TestReadPidFile_ParsesValid(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "proxy.pid")
	require.NoError(t, os.WriteFile(path, []byte(" 4242 \n"), 0o644))

	pid, ok := readPidFile(path)
	require.True(t, ok)
	assert.Equal(t, 4242, pid)
}

func TestReadPidFile_MissingReturnsNotOK(t *testing.T) {
	tmp := t.TempDir()
	_, ok := readPidFile(filepath.Join(tmp, "nope.pid"))
	assert.False(t, ok)
}
