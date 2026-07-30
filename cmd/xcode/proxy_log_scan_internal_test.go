//go:build unit

package xcode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
)

func TestScanProxyLog_CountsErrorsAndKeepsDistinctSamples(t *testing.T) {
	logLines := strings.Join([]string{
		`[INFO] proxy listening on /Users/x/.bitrise-xcelerate/proxy.sock`,
		`[DEBUG] get 8ab31f9c0d1e2f3a hit: false`,
		`[ERROR] get 8ab31f9c0d1e2f3a: rpc error: code = Unavailable desc = connection refused`,
		`[ERROR] get 1122334455667788: rpc error: code = Unavailable desc = connection refused`,
		`[ERROR] put deadline exceeded after 10s`,
		`[DEBUG] session stats: 0 hits`,
	}, "\n")

	got := scanProxyLog(strings.NewReader(logLines))

	assert.Equal(t, 3, got.Errors)
	require.Len(t, got.Samples, 2, "the two connection-refused lines differ only by key")
	assert.Contains(t, got.Samples[0], "connection refused")
	assert.Contains(t, got.Samples[1], "deadline exceeded")
}

// "hit: false" and a key that happens to spell a keyword must not register.
func TestScanProxyLog_IgnoresHealthyLines(t *testing.T) {
	got := scanProxyLog(strings.NewReader(strings.Join([]string{
		`[DEBUG] get abcdef0123456789 hit: false`,
		`[DEBUG] put abcdef0123456789 ok (1.2 MB)`,
		`[INFO] 412 hits / 2746 lookups`,
	}, "\n")))

	assert.Equal(t, 0, got.Errors)
	assert.False(t, got.any())
}

func TestScanProxyLog_CapsSamplesButKeepsCounting(t *testing.T) {
	var b strings.Builder
	for i := range 500 {
		b.WriteString("[ERROR] distinct failure ")
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(" unavailable\n")
	}

	got := scanProxyLog(strings.NewReader(b.String()))

	assert.Equal(t, 500, got.Errors)
	assert.Len(t, got.Samples, proxyErrorSampleMax)
}

// The error log outlives a build, so a previous build's failures must not be
// attributed to this one.
func TestReadProxyStderrSince_SkipsWhatWasThereBefore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-err.log")
	require.NoError(t, os.WriteFile(path, []byte("failure from an earlier build\n"), 0o600))

	offset := fileSize(path)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("panic: listen unix: address already in use\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	got := readProxyStderrSince(path, offset)

	assert.Equal(t, "panic: listen unix: address already in use", got)
	assert.NotContains(t, got, "earlier build")
}

func TestReadProxyStderrSince_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.log")

	assert.Equal(t, int64(0), fileSize(path))
	assert.Empty(t, readProxyStderrSince(path, 0))
}

func TestXcodeDoctor_ReportsProxyLogErrors(t *testing.T) {
	var out strings.Builder
	osProxy, home := tempHomeProxy(t)
	dir := proxyLogDir(t, home)

	const invocationID = "1f2e3d4c"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "proxy-"+invocationID+"-out.log"),
		[]byte("[ERROR] get aabbccddeeff0011: rpc error: code = Unauthenticated desc = token expired\n"),
		0o600))

	d := &xcodeDoctor{
		Logger:       log.NewLogger(log.WithOutput(&out)),
		OsProxy:      osProxy,
		CacheEnabled: true,
		InvocationID: invocationID,
		RunChecks: func(context.Context, doctorpkg.Options) doctorpkg.Report {
			return okReport("auth")
		},
	}

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), xcodeargs.CompCacheStats{})

	logged := out.String()
	assert.Contains(t, logged, "1 error line(s)")
	assert.Contains(t, logged, "token expired")
	assert.Contains(t, logged, "proxy-"+invocationID+"-out.log")
}

// A build whose proxy log is clean must not print a proxy warning, even though
// the file exists.
func TestXcodeDoctor_SilentOnCleanProxyLog(t *testing.T) {
	var out strings.Builder
	osProxy, home := tempHomeProxy(t)
	dir := proxyLogDir(t, home)

	const invocationID = "99887766"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "proxy-"+invocationID+"-out.log"),
		[]byte("[DEBUG] get aabbccddeeff0011 hit: true\n"),
		0o600))

	d := &xcodeDoctor{
		Logger:       log.NewLogger(log.WithOutput(&out)),
		OsProxy:      osProxy,
		CacheEnabled: true,
		InvocationID: invocationID,
		RunChecks: func(context.Context, doctorpkg.Options) doctorpkg.Report {
			return okReport("auth")
		},
	}

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), xcodeargs.CompCacheStats{})

	assert.Empty(t, out.String())
}

// A proxy that dies before it can open a per-invocation log leaves its reason
// only in the shared error log.
func TestXcodeDoctor_ReportsProxyStderrWrittenDuringBuild(t *testing.T) {
	var out strings.Builder
	osProxy, home := tempHomeProxy(t)
	dir := proxyLogDir(t, home)
	errLog := filepath.Join(dir, "proxy-err.log")

	require.NoError(t, os.WriteFile(errLog, []byte("stale failure from yesterday\n"), 0o600))

	d := &xcodeDoctor{
		Logger:       log.NewLogger(log.WithOutput(&out)),
		OsProxy:      osProxy,
		CacheEnabled: true,
		InvocationID: "abc123",
		RunChecks: func(context.Context, doctorpkg.Options) doctorpkg.Report {
			return okReport("auth")
		},
	}

	d.CheckAtStart(context.Background())

	f, err := os.OpenFile(errLog, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("panic: failed to dial the build cache\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	d.ReportAtEnd(context.Background(), xcodeargs.CompCacheStats{})

	logged := out.String()
	assert.Contains(t, logged, msgDoctorProxyStderr)
	assert.Contains(t, logged, "failed to dial the build cache")
	assert.NotContains(t, logged, "yesterday", "the shared log outlives a single build")
}
