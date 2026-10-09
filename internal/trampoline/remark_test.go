//go:build unit

package trampoline

import (
	"bytes"
	"os"
	"os/exec"
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

// TestEmitEngagedRemark_CrossProcessDedup proves the sentinel survives process
// exit (unlike flock-held-by-fd), so a shim that execs its target still blocks
// a sibling shim from re-emitting the remark.
func TestEmitEngagedRemark_CrossProcessDedup(t *testing.T) {
	if os.Getenv("TEST_HELPER_PROCESS") == "1" {
		runHelperEmitRemark()
		os.Exit(0)
	}

	tmp := t.TempDir()
	helperEnv := []string{
		"TEST_HELPER_PROCESS=1",
		"HELPER_SENTINEL_DIR=" + tmp,
		"HELPER_PID=7777",
	}

	out1, err1 := runHelper(t, helperEnv)
	require.NoError(t, err1, "first helper exit: %s", out1)
	out2, err2 := runHelper(t, helperEnv)
	require.NoError(t, err2, "second helper exit: %s", out2)

	assert.Contains(t, out1, remarkText, "first process must emit remark")
	assert.NotContains(t, out2, remarkText, "second process must skip (sentinel from first survived process exit)")

	sentinel := filepath.Join(tmp, remarkSentinelPrefix+"7777")
	_, err := os.Stat(sentinel)
	assert.NoError(t, err, "sentinel must persist across process exit")
}

func runHelper(t *testing.T, env []string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestEmitEngagedRemark_CrossProcessDedup")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()

	return string(out), err
}

func runHelperEmitRemark() {
	tmp := os.Getenv("HELPER_SENTINEL_DIR")
	pid, _ := strconv.Atoi(os.Getenv("HELPER_PID"))
	emitEngagedRemark(remarkDeps{
		readProxyPid:   func() (int, bool) { return pid, true },
		proxyStartTime: func(int) (int64, bool) { return 0, false },
		createSentinel: defaultRemarkDeps.createSentinel,
		stderr:         os.Stdout,
		tmpDir:         func() string { return tmp },
	})
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
