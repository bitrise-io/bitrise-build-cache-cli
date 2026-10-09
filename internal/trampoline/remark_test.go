//go:build unit

package trampoline

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseRemarkDeps(t *testing.T, pid int) (*bytes.Buffer, remarkDeps) {
	t.Helper()
	buf := &bytes.Buffer{}
	tmp := t.TempDir()

	return buf, remarkDeps{
		readProxyPid:   func() (int, bool) { return pid, pid > 0 },
		proxyStartTime: func(int) (int64, bool) { return 0, false },
		createSentinel: defaultRemarkDeps.createSentinel,
		stderr:         buf,
		tmpDir:         func() string { return tmp },
	}
}

func TestEmitEngagedRemark_WritesRemarkOnFirstCall(t *testing.T) {
	buf, d := baseRemarkDeps(t, 1234)

	emitEngagedRemark(d)

	assert.Contains(t, buf.String(), remarkText)
}

func TestEmitEngagedRemark_SkipsOnSecondCall(t *testing.T) {
	buf, d := baseRemarkDeps(t, 1234)

	emitEngagedRemark(d)
	buf.Reset()
	emitEngagedRemark(d)

	assert.Empty(t, buf.String(), "sentinel exists after first call; second must skip")
}

func TestEmitEngagedRemark_SkipsWhenProxyPidMissing(t *testing.T) {
	buf, d := baseRemarkDeps(t, 0)

	emitEngagedRemark(d)

	assert.Empty(t, buf.String(), "no proxy pid means no proxy up yet; shim must not emit")
}

func TestEmitEngagedRemark_SentinelPathIsPIDAndStartTimeScoped(t *testing.T) {
	buf, d := baseRemarkDeps(t, 42)
	d.proxyStartTime = func(int) (int64, bool) { return 1700000000000, true }
	var observed string
	d.createSentinel = func(path string) (bool, error) {
		observed = path

		return true, nil
	}

	emitEngagedRemark(d)

	assert.Contains(t, observed, "bitrise-xcelerate-remark-42-1700000000000")
	assert.NotEmpty(t, buf.String())
}

func TestEmitEngagedRemark_SentinelFallsBackToPIDOnlyWhenStartTimeUnresolved(t *testing.T) {
	buf, d := baseRemarkDeps(t, 99)
	var observed string
	d.createSentinel = func(path string) (bool, error) {
		observed = path

		return true, nil
	}

	emitEngagedRemark(d)

	assert.Equal(t, filepath.Join(filepath.Dir(observed), "bitrise-xcelerate-remark-99"), observed)
	assert.NotEmpty(t, buf.String())
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
