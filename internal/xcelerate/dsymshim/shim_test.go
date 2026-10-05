//go:build unit

package dsymshim

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type recordingExec struct {
	calls [][]string
	// stdoutLines emitted by the fake dsymutil to its stdout.
	stdoutLines []string
	// stderrLines emitted by the fake dsymutil to its stderr.
	stderrLines []string
	exitCode    int
}

func (r *recordingExec) command(ctx context.Context, name string, argv ...string) *exec.Cmd {
	call := append([]string{name}, argv...)
	r.calls = append(r.calls, call)

	// Build a sh -c script that mirrors the configured outputs then exits with the code.
	script := ""
	for _, l := range r.stdoutLines {
		script += "echo " + shQuote(l) + ";"
	}
	for _, l := range r.stderrLines {
		script += "echo " + shQuote(l) + " >&2;"
	}
	script += "exit " + itoa(r.exitCode)

	return exec.CommandContext(ctx, "/bin/sh", "-c", script) //nolint:gosec // test fake
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	// Small ints only.
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 4)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		return "-" + string(buf)
	}

	return string(buf)
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func newTempParams(t *testing.T) (paths.Paths, *recordingExec, *os.File, *os.File) {
	t.Helper()
	home := t.TempDir()
	p := paths.FromHome(home)
	// Point xcelerate state dir at a tempdir-sibling, matching paths.FromHome semantics.
	rec := &recordingExec{exitCode: 0}
	stdout, err := os.CreateTemp(t.TempDir(), "stdout-*")
	require.NoError(t, err)
	stderr, err := os.CreateTemp(t.TempDir(), "stderr-*")
	require.NoError(t, err)

	return p, rec, stdout, stderr
}

func readTouchFiles(t *testing.T, dir string) []touchRecord {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	out := make([]touchRecord, 0, len(entries))
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			var rec touchRecord
			require.NoError(t, json.Unmarshal([]byte(line), &rec))
			out = append(out, rec)
		}
	}

	return out
}

func TestShim_KillSwitchBypass(t *testing.T) {
	p, rec, stdout, stderr := newTempParams(t)
	defer stdout.Close()
	defer stderr.Close()

	params := Params{
		Argv:    []string{"/path/to/binary", "-o", "/path/to/out.dSYM"},
		Env:     map[string]string{EnvKillSwitch: "1"},
		Stdin:   os.Stdin,
		Stdout:  stdout,
		Stderr:  stderr,
		Logger:  log.NewLogger(),
		OsProxy: utils.DefaultOsProxy{},
		Paths:   p,
		// Force-feature-probe to a no-op so we don't shell out to a real dsymutil.
		FeatureProbe: func(_ context.Context, _ string) (bool, string, error) {
			t.Fatalf("feature probe must not run under killswitch")

			return false, "", nil
		},
		ExecCommand:       rec.command,
		StockDsymutilPath: "/stock/dsymutil",
	}

	exit := params.Run(context.Background())
	assert.Equal(t, 0, exit)
	require.Len(t, rec.calls, 1)
	assert.Equal(t, "/stock/dsymutil", rec.calls[0][0])
	assert.Equal(t, []string{"/path/to/binary", "-o", "/path/to/out.dSYM"}, rec.calls[0][1:])

	touches := readTouchFiles(t, p.XcelerateDsymShimDir())
	require.Len(t, touches, 1)
	assert.Equal(t, "bypass:killswitch", touches[0].Mode)
}

func TestShim_IdempotentPassthroughWhenCASFlagsPresent(t *testing.T) {
	p, rec, stdout, stderr := newTempParams(t)
	defer stdout.Close()
	defer stderr.Close()

	params := Params{
		Argv:              []string{"/bin.o", "-cas-plugin-path", "/some/dylib", "-o", "/x.dSYM"},
		Env:               map[string]string{},
		Stdout:            stdout,
		Stderr:            stderr,
		Logger:            log.NewLogger(),
		OsProxy:           utils.DefaultOsProxy{},
		Paths:             p,
		ExecCommand:       rec.command,
		StockDsymutilPath: "/stock/dsymutil",
	}

	exit := params.Run(context.Background())
	assert.Equal(t, 0, exit)
	require.Len(t, rec.calls, 1)
	// Must pass the argv unmodified.
	assert.Equal(t, params.Argv, rec.calls[0][1:])

	touches := readTouchFiles(t, p.XcelerateDsymShimDir())
	require.Len(t, touches, 1)
	assert.Equal(t, "bypass:already-wired", touches[0].Mode)
}

func TestShim_FeatureProbeBypassOnNoSupport(t *testing.T) {
	p, rec, stdout, stderr := newTempParams(t)
	defer stdout.Close()
	defer stderr.Close()

	params := Params{
		Argv:              []string{"/bin.o", "-o", "/x.dSYM"},
		Env:               map[string]string{},
		Stdout:            stdout,
		Stderr:            stderr,
		Logger:            log.NewLogger(),
		OsProxy:           utils.DefaultOsProxy{},
		Paths:             p,
		FeatureProbe:      func(_ context.Context, _ string) (bool, string, error) { return false, "fake-ver", nil },
		ExecCommand:       rec.command,
		StockDsymutilPath: "/stock/dsymutil",
	}

	exit := params.Run(context.Background())
	assert.Equal(t, 0, exit)
	require.Len(t, rec.calls, 1)
	assert.Equal(t, params.Argv, rec.calls[0][1:])

	touches := readTouchFiles(t, p.XcelerateDsymShimDir())
	require.Len(t, touches, 1)
	assert.Equal(t, "bypass:no-cas-support", touches[0].Mode)
}

func TestShim_HappyPathPrependsFlagsAndWritesTouch(t *testing.T) {
	p, rec, stdout, stderr := newTempParams(t)
	defer stdout.Close()
	defer stderr.Close()

	// A plugin file we can Stat (just make an empty file inside the test tempdir).
	fakePlugin := filepath.Join(t.TempDir(), "libCAS.dylib")
	require.NoError(t, os.WriteFile(fakePlugin, []byte("x"), 0o644))

	params := Params{
		Argv: []string{"/bin.o", "-o", "/x.dSYM"},
		Env: map[string]string{
			EnvPluginPathOverride: fakePlugin,
			EnvCASPathOverride:    "/some/cas",
		},
		Stdout:            stdout,
		Stderr:            stderr,
		Logger:            log.NewLogger(),
		OsProxy:           utils.DefaultOsProxy{},
		Paths:             p,
		FeatureProbe:      func(_ context.Context, _ string) (bool, string, error) { return true, "fake-ver", nil },
		ExecCommand:       rec.command,
		StockDsymutilPath: "/stock/dsymutil",
	}

	exit := params.Run(context.Background())
	assert.Equal(t, 0, exit)

	require.Len(t, rec.calls, 1)
	// Expect -cas-plugin-path first, then -cas, then user argv.
	wantPrefix := []string{"-cas-plugin-path", fakePlugin, "-cas", "/some/cas", "/bin.o", "-o", "/x.dSYM"}
	assert.Equal(t, wantPrefix, rec.calls[0][1:])

	touches := readTouchFiles(t, p.XcelerateDsymShimDir())
	require.Len(t, touches, 1)
	assert.Equal(t, "shim", touches[0].Mode)
	assert.Equal(t, fakePlugin, touches[0].PluginPath)
	assert.Equal(t, "/some/cas", touches[0].CasPath)
	assert.Equal(t, 0, touches[0].ExitCode)
}

func TestShim_HappyPathWithStderrFilteringCountsPairs(t *testing.T) {
	p, _, stdout, stderr := newTempParams(t)
	defer stdout.Close()
	defer stderr.Close()

	fakePlugin := filepath.Join(t.TempDir(), "libCAS.dylib")
	require.NoError(t, os.WriteFile(fakePlugin, []byte("x"), 0o644))

	rec := &recordingExec{
		exitCode: 0,
		stderrLines: []string{
			"warning: 0~ABC==: No such file or directory",
			"note: while processing 0~ABC==",
			"info: just an info line",
		},
	}

	params := Params{
		Argv:              []string{"/bin.o", "-o", "/x.dSYM"},
		Env:               map[string]string{EnvPluginPathOverride: fakePlugin},
		Stdout:            stdout,
		Stderr:            stderr,
		Logger:            log.NewLogger(),
		OsProxy:           utils.DefaultOsProxy{},
		Paths:             p,
		FeatureProbe:      func(_ context.Context, _ string) (bool, string, error) { return true, "fake-ver", nil },
		ExecCommand:       rec.command,
		StockDsymutilPath: "/stock/dsymutil",
	}

	exit := params.Run(context.Background())
	assert.Equal(t, 0, exit)

	touches := readTouchFiles(t, p.XcelerateDsymShimDir())
	require.Len(t, touches, 1)
	// One paired warning stripped, two stderr lines removed.
	assert.Equal(t, 1, touches[0].MissedCount)
	assert.Equal(t, 2, touches[0].FilteredStderr)

	// Rewind stderr file and check the info line survived + the summary line is present.
	_, err := stderr.Seek(0, 0)
	require.NoError(t, err)
	stderrContent := make([]byte, 4096)
	n, _ := stderr.Read(stderrContent)
	got := string(stderrContent[:n])
	assert.Contains(t, got, "info: just an info line")
	assert.Contains(t, got, SummaryToken)
	assert.NotContains(t, got, "warning: 0~ABC==")
}
