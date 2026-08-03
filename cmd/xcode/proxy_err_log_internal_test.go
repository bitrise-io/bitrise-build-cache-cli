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

func TestXcodeDoctor_ReportsProxyErrorsFromTheStats(t *testing.T) {
	var out strings.Builder
	osProxy, home := tempHomeProxy(t)
	proxyLogDir(t, home)

	const invocationID = "1f2e3d4c"
	logger := log.NewLogger(log.WithOutput(&out))
	logger.EnableDebugLog(true)
	d := &xcodeDoctor{
		Logger:       logger,
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
	assert.Contains(t, logged, "3 error(s)")
	assert.Contains(t, logged, "bitrise-build-cache doctor")
	// The proxy's own wording and paths are debug-only: a build log shouldn't
	// explain the pieces to someone who just wants their cache working.
	assert.Contains(t, logged, "token expired")
	assert.Contains(t, logged, "proxy-"+invocationID+"-out.log")
}

// Without --debug, the same build says only what happened and what to run.
func TestXcodeDoctor_KeepsProxyInternalsOutOfTheBuildOutput(t *testing.T) {
	var out strings.Builder
	osProxy, home := tempHomeProxy(t)
	proxyLogDir(t, home)

	d := &xcodeDoctor{
		Logger:       log.NewLogger(log.WithOutput(&out)),
		OsProxy:      osProxy,
		CacheEnabled: true,
		InvocationID: "1f2e3d4c",
		RunChecks: func(context.Context, doctorpkg.Options) doctorpkg.Report {
			return okReport("auth")
		},
	}

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), buildOutcome{
		CAS:   xcodeargs.CompCacheStats{CASErrors: 4002},
		Proxy: proxyOutcome{Errors: 3, FirstError: "Get: token expired"},
	})

	logged := out.String()
	assert.Contains(t, logged, "4002 error(s)", "the count the build actually felt")
	for _, internal := range []string{"proxy", "CAS", "socket", "token expired", ".log"} {
		assert.NotContains(t, logged, internal)
	}
}

// Error-shaped log lines are not a failure; only the proxy's count is.
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

// A proxy that stopped answering gets a plain warning; its own words go to the
// debug log, which is also the only place the shared error log is quoted.
func TestXcodeDoctor_ReportsProxyStderrWrittenDuringBuild(t *testing.T) {
	var out strings.Builder
	osProxy, home := tempHomeProxy(t)
	dir := proxyLogDir(t, home)
	errLog := filepath.Join(dir, "proxy-err.log")

	require.NoError(t, os.WriteFile(errLog, []byte("stale failure from yesterday\n"), 0o600))

	logger := log.NewLogger(log.WithOutput(&out))
	logger.EnableDebugLog(true)
	d := &xcodeDoctor{
		Logger:       logger,
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
	assert.Contains(t, logged, msgDoctorCacheStopped)
	assert.Contains(t, logged, "failed to dial the build cache")
	assert.NotContains(t, logged, "yesterday", "the shared log outlives a single build")
}
