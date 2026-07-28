//go:build unit

package xcode

import (
	"context"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
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

	d := newXcodeDoctor(log.NewLogger(log.WithOutput(&out)), false)
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

func TestXcodeDoctor_HealthySetupIsSilent(t *testing.T) {
	d, out, _ := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	})

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background())

	assert.Empty(t, out.String())
}

// A clean run still has to leave a trace at debug level, so a build log shows
// the checks ran at all.
func TestXcodeDoctor_HealthySetupLogsAtDebug(t *testing.T) {
	var out strings.Builder
	logger := log.NewLogger(log.WithOutput(&out))
	logger.EnableDebugLog(true)

	d := newXcodeDoctor(logger, false)
	d.RunChecks = func(context.Context, doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	}

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background())

	logged := out.String()
	assert.Contains(t, logged, "Running health checks")
	assert.Contains(t, logged, "ok auth: fine")
	assert.Contains(t, logged, "Health check result: ok")
	assert.Contains(t, logged, "No health-check issues to repeat")
}

func TestXcodeDoctor_ReportAtEndRepeatsStartIssues(t *testing.T) {
	d, out, opts := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return warnReport("auth", "token expired")
	})

	d.CheckAtStart(context.Background())
	out.Reset()
	d.ReportAtEnd(context.Background())

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

	out.Reset()
	d.ReportAtEnd(context.Background())
	assert.Empty(t, out.String(), "the probe verdict supersedes the start-of-build report")
}

func TestXcodeDoctor_SaveFailureWithHealthyAuth(t *testing.T) {
	d, out, _ := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth-backend")
	})

	d.OnInvocationSaveFailure(context.Background())

	assert.Contains(t, out.String(), msgDoctorProbeNoIssues)
	assert.NotContains(t, out.String(), msgDoctorIssuesFound)
}
