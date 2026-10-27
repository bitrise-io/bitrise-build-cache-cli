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

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildidentity"
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
		ancestry:       func() []buildidentity.AncestryEntry { return nil },
	}
}

// testHostname mirrors buildidentity.hostname()'s os.Hostname() lookup. Local
// to the test because the production helper is unexported.
func testHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}

	return h
}

func anchorableAncestry() []buildidentity.AncestryEntry {
	return []buildidentity.AncestryEntry{
		{PID: 100, Name: "swiftc", StartTimeMS: 1},
		{PID: 101, Name: "xcodebuild", StartTimeMS: 2},
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
	// Capture only the engaged-sentinel path by filtering out visit-URL sentinels.
	var observed string
	d.createSentinel = func(path string) (bool, error) {
		if filepath.Base(path) == paths.TrampolineRemarkSentinelPrefix+"99" {
			observed = path
		}

		return true, nil
	}

	emitEngagedRemark(d)

	assert.Equal(t, filepath.Join(filepath.Dir(observed), "bitrise-xcelerate-remark-99"), observed)
	assert.NotEmpty(t, buf.String())
}

// TestEmitEngagedRemark_PredictedVisitURL covers the happy path: ancestry
// resolves to an xcodebuild anchor and the Visit URL remark is appended after
// the engaged line.
func TestEmitEngagedRemark_PredictedVisitURL(t *testing.T) {
	buf, d := baseRemarkDeps(t, 1234)
	d.ancestry = anchorableAncestry

	expectedID := buildidentity.Derive(buildidentity.Anchor{Hostname: testHostname(), PID: 101, StartTimeMS: 2})

	emitEngagedRemark(d)

	assert.Contains(t, buf.String(), remarkText)
	assert.Contains(t, buf.String(), "remark: Invocation saved. Visit 👉 https://app.bitrise.io/build-cache/invocations/xcode/"+expectedID)
}

// TestEmitEngagedRemark_NoAnchor covers the fallback: ancestry contains only
// non-anchorable names, so the engaged line emits but no Visit URL is printed.
func TestEmitEngagedRemark_NoAnchor(t *testing.T) {
	buf, d := baseRemarkDeps(t, 1234)
	d.ancestry = func() []buildidentity.AncestryEntry {
		return []buildidentity.AncestryEntry{
			{PID: 42, Name: "sh", StartTimeMS: 1},
			{PID: 43, Name: "zsh", StartTimeMS: 2},
		}
	}

	emitEngagedRemark(d)

	assert.Contains(t, buf.String(), remarkText)
	assert.NotContains(t, buf.String(), "Visit 👉")
}

// TestEmitEngagedRemark_DedupByInvocationID proves two calls with the same
// anchorable ancestry print the engaged line + Visit URL once each.
func TestEmitEngagedRemark_DedupByInvocationID(t *testing.T) {
	buf, d := baseRemarkDeps(t, 1234)
	d.ancestry = anchorableAncestry

	emitEngagedRemark(d)
	firstOut := buf.String()

	buf.Reset()
	emitEngagedRemark(d)

	assert.Contains(t, firstOut, remarkText)
	assert.Contains(t, firstOut, "Visit 👉")
	assert.Empty(t, buf.String(), "both sentinels exist after first call; second must skip entirely")
}

// TestEmitEngagedRemark_FreshInvocationIDFiresURL proves two calls with
// different anchors each print their own Visit URL (one per anchor).
func TestEmitEngagedRemark_FreshInvocationIDFiresURL(t *testing.T) {
	buf, d := baseRemarkDeps(t, 1234)
	calls := 0
	d.ancestry = func() []buildidentity.AncestryEntry {
		calls++
		if calls == 1 {
			return []buildidentity.AncestryEntry{{PID: 101, Name: "xcodebuild", StartTimeMS: 2}}
		}

		return []buildidentity.AncestryEntry{{PID: 999, Name: "xcodebuild", StartTimeMS: 999}}
	}

	id1 := buildidentity.Derive(buildidentity.Anchor{Hostname: testHostname(), PID: 101, StartTimeMS: 2})
	id2 := buildidentity.Derive(buildidentity.Anchor{Hostname: testHostname(), PID: 999, StartTimeMS: 999})

	emitEngagedRemark(d)
	firstOut := buf.String()

	buf.Reset()
	emitEngagedRemark(d)
	secondOut := buf.String()

	assert.Contains(t, firstOut, id1)
	assert.Contains(t, secondOut, id2)
	assert.NotContains(t, secondOut, remarkText, "engaged line sentinel is proxy-PID-scoped; second must skip")
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

	sentinel := filepath.Join(tmp, paths.TrampolineRemarkSentinelPrefix+"7777")
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
		ancestry:       func() []buildidentity.AncestryEntry { return nil },
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
