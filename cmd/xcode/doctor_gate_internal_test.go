//go:build unit

package xcode

import (
	"context"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
)

func warnReport(name, detail string) doctorpkg.Report {
	return doctorpkg.Report{Items: []doctorpkg.ReportItem{
		{Name: name, Result: doctorpkg.Result{State: doctorpkg.StateWarn, Detail: detail}},
	}}
}

func okReport(name string) doctorpkg.Report {
	return doctorpkg.Report{Items: []doctorpkg.ReportItem{
		{Name: name, Result: doctorpkg.Result{State: doctorpkg.StateOK, Detail: "fine"}},
	}}
}

func newTestDoctor(report func(doctorpkg.Options) doctorpkg.Report) (*xcodeDoctor, *strings.Builder, *[]doctorpkg.Options) {
	var out strings.Builder
	opts := &[]doctorpkg.Options{}

	d := newXcodeDoctor(log.NewLogger(log.WithOutput(&out)), false, true, configcommon.CacheAuthConfig{})
	d.RunChecks = func(_ context.Context, o doctorpkg.Options) doctorpkg.Report {
		*opts = append(*opts, o)

		return report(o)
	}

	return d, &out, opts
}

func TestXcodeDoctor_StartCheckSkipsNetworkCalls(t *testing.T) {
	d, out, opts := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return warnReport("xcelerate-proxy", "not running (no socket file)")
	})

	d.CheckAtStart(context.Background())

	require.Len(t, *opts, 1)
	assert.Equal(t, doctorpkg.XcodeCheckNames, (*opts)[0].Only)
	assert.True(t, (*opts)[0].SkipBackendProbe, "no network round-trip per build")
	assert.True(t, (*opts)[0].SkipUpdateCheck)
	assert.False(t, (*opts)[0].ApplyFixes, "a build must not mutate the setup")

	assert.Contains(t, out.String(), msgDoctorIssuesFound)
	assert.Contains(t, out.String(), "xcelerate-proxy: not running (no socket file)")
}

// With the cache off (a baseline benchmark phase, or --no-bitrise-build-cache)
// no proxy is started, so reporting it as down would be noise — but the
// analytics PUT still needs auth, so auth must stay in the set.
func TestXcodeDoctor_CacheOffDropsProxyCheckButKeepsAuth(t *testing.T) {
	var out strings.Builder
	var opts []doctorpkg.Options

	d := newXcodeDoctor(log.NewLogger(log.WithOutput(&out)), false, false, configcommon.CacheAuthConfig{})
	d.RunChecks = func(_ context.Context, o doctorpkg.Options) doctorpkg.Report {
		opts = append(opts, o)

		return warnReport("auth", "no credentials found")
	}

	d.CheckAtStart(context.Background())

	require.Len(t, opts, 1)
	assert.Equal(t, doctorpkg.XcodeAnalyticsOnlyCheckNames, opts[0].Only)
	assert.NotContains(t, opts[0].Only, "xcelerate-proxy")
	assert.Contains(t, opts[0].Only, "auth")
	assert.Contains(t, out.String(), "auth: no credentials found")
}

func TestXcodeDoctor_HealthySetupIsSilent(t *testing.T) {
	d, out, _ := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	})

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), xcodeargs.CompCacheStats{})

	assert.Empty(t, out.String())
}

// A clean run still has to leave a trace at debug level, so a build log shows
// the checks ran at all.
func TestXcodeDoctor_HealthySetupLogsAtDebug(t *testing.T) {
	var out strings.Builder
	logger := log.NewLogger(log.WithOutput(&out))
	logger.EnableDebugLog(true)

	d := newXcodeDoctor(logger, false, true, configcommon.CacheAuthConfig{})
	d.RunChecks = func(context.Context, doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	}

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), xcodeargs.CompCacheStats{})

	logged := out.String()
	assert.Contains(t, logged, "Running health checks")
	assert.Contains(t, logged, "ok auth: fine")
	assert.Contains(t, logged, "Health check result: ok")
	assert.Contains(t, logged, "found no issues at the start")
}

func TestXcodeDoctor_ReportAtEndRepeatsStartIssues(t *testing.T) {
	d, out, opts := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return warnReport("auth", "token expired")
	})

	d.CheckAtStart(context.Background())
	out.Reset()
	d.ReportAtEnd(context.Background(), xcodeargs.CompCacheStats{})

	assert.Len(t, *opts, 1, "the end-of-build recap must not re-run the checks")
	assert.Contains(t, out.String(), msgDoctorIssuesRecap)
	assert.Contains(t, out.String(), "auth: token expired")
}

func TestXcodeDoctor_SaveFailureProbesBackendAndDropsStartBuffer(t *testing.T) {
	d, out, opts := newTestDoctor(func(o doctorpkg.Options) doctorpkg.Report {
		if o.SkipBackendProbe {
			return warnReport("xcelerate-proxy", "not running (no socket file)")
		}

		return warnReport("auth-backend", "unauthorized (token expired)")
	})

	d.CheckAtStart(context.Background())
	out.Reset()

	d.OnInvocationSaveFailure(context.Background())

	require.Len(t, *opts, 2)
	assert.Equal(t, doctorpkg.AuthProbeCheckNames, (*opts)[1].Only)
	assert.False(t, (*opts)[1].SkipBackendProbe, "the probe is the point of this run")

	assert.Contains(t, out.String(), msgDoctorProbingAuth)
	assert.Contains(t, out.String(), "auth-backend: unauthorized (token expired)")

	// The probe has to test the credential the failed PUT used, not whatever else
	// the machine can resolve — a CI JWT would otherwise report healthy.
	require.NotNil(t, (*opts)[1].PinAuth)
	assert.Nil(t, (*opts)[0].PinAuth, "the start check makes no backend call to pin")

	out.Reset()
	d.ReportAtEnd(context.Background(), xcodeargs.CompCacheStats{})
	assert.NotContains(t, out.String(), msgDoctorIssuesRecap,
		"the probe verdict supersedes the start-of-build report")
	assert.NotContains(t, out.String(), "found no issues",
		"issues were found — the recap must not claim otherwise")
}

func TestXcodeDoctor_ReportAtEndWarnsOnCASErrors(t *testing.T) {
	d, out, _ := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	})

	d.CheckAtStart(context.Background())
	out.Reset()
	d.ReportAtEnd(context.Background(), xcodeargs.CompCacheStats{Hits: 0, TotalTasks: 2746, CASErrors: 4002})

	assert.Contains(t, out.String(), "4002 error(s)")
	assert.Contains(t, out.String(), "compiled locally")
	assert.Contains(t, out.String(), "stop-proxy")
}

// A cold first build is 0% with no errors and must stay silent.
func TestXcodeDoctor_ReportAtEndSilentOnColdCacheWithoutErrors(t *testing.T) {
	d, out, _ := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	})

	d.CheckAtStart(context.Background())
	out.Reset()
	d.ReportAtEnd(context.Background(), xcodeargs.CompCacheStats{Hits: 0, TotalTasks: 2746, CASErrors: 0})

	assert.Empty(t, out.String())
}

func TestXcodeDoctor_SaveFailureWithHealthyAuth(t *testing.T) {
	d, out, _ := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth-backend")
	})

	d.OnInvocationSaveFailure(context.Background())

	assert.Contains(t, out.String(), msgDoctorProbeNoIssues)
	assert.NotContains(t, out.String(), msgDoctorIssuesFound)
}
