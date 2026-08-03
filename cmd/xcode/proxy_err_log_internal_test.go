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
)

// The count and the reason both come from the proxy, so the report needs no log
// parsing at all — only the path, for a reader who wants the rest.
func TestXcodeDoctor_ReportsProxyErrorsFromTheStats(t *testing.T) {
	var out strings.Builder
	osProxy, home := tempHomeProxy(t)
	proxyLogDir(t, home)

	const invocationID = "1f2e3d4c"
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
	d.ReportAtEnd(context.Background(), buildOutcome{Proxy: proxyOutcome{
		Errors:     3,
		FirstError: "Get: rpc error: code = Unauthenticated desc = token expired",
	}})

	logged := out.String()
	assert.Contains(t, logged, "3 request(s)")
	assert.Contains(t, logged, "token expired")
	assert.Contains(t, logged, "proxy-"+invocationID+"-out.log")
}

// A build where the proxy completed everything says nothing, whatever its log
// happens to contain — the regex that used to decide this made any line with the
// word "error" in it a build warning.
func TestXcodeDoctor_SilentWhenTheProxyReportsNoFailures(t *testing.T) {
	var out strings.Builder
	osProxy, home := tempHomeProxy(t)
	dir := proxyLogDir(t, home)

	const invocationID = "55667788"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "proxy-"+invocationID+"-out.log"),
		[]byte("[DEBUG] retrying after transient error, recovered\n[DEBUG] get aabb hit: true\n"),
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
	d.ReportAtEnd(context.Background(), buildOutcome{})

	assert.Empty(t, out.String())
}

// A proxy that stopped answering has no counters left to report, so its shared
// error log is the only evidence of why — the one job the log still has.
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

	d.ReportAtEnd(context.Background(), buildOutcome{Proxy: proxyOutcome{Unreachable: true}})

	logged := out.String()
	assert.Contains(t, logged, msgDoctorProxyUnreachable)
	assert.Contains(t, logged, msgDoctorProxyStderr)
	assert.Contains(t, logged, "failed to dial the build cache")
	assert.NotContains(t, logged, "yesterday", "the shared log outlives a single build")
}
