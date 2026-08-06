//go:build unit

package reactnative

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

func newTestDoctor(report func(doctorpkg.Options) doctorpkg.Report) (*rnDoctor, *strings.Builder, *[]doctorpkg.Options) {
	var out strings.Builder
	opts := &[]doctorpkg.Options{}

	d := &rnDoctor{
		Logger: log.NewLogger(log.WithOutput(&out)),
		RunChecks: func(_ context.Context, o doctorpkg.Options) doctorpkg.Report {
			*opts = append(*opts, o)

			return report(o)
		},
	}

	return d, &out, opts
}

func TestRNDoctor_StartCheckSkipsNetworkCallsAndFixes(t *testing.T) {
	d, out, opts := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return warnReport("ccache-helper", "not running (no socket file)")
	})

	d.CheckAtStart(context.Background())

	require.Len(t, *opts, 1)
	assert.Equal(t, doctorpkg.ReactNativeCheckNames, (*opts)[0].Only)
	assert.True(t, (*opts)[0].SkipBackendProbe, "no network round-trip per build")
	assert.True(t, (*opts)[0].SkipUpdateCheck)
	assert.False(t, (*opts)[0].ApplyFixes, "a build must not mutate the setup")

	assert.Contains(t, out.String(), msgDoctorIssuesFound)
	assert.Contains(t, out.String(), "ccache-helper: not running (no socket file)")
}

// The nested xcodebuild wrapper starts the proxy after this check runs, so a
// proxy that is down here is not a problem worth reporting.
func TestRNDoctor_StartCheckSetExcludesTheProxy(t *testing.T) {
	assert.NotContains(t, doctorpkg.ReactNativeCheckNames, "xcelerate-proxy")
	assert.Contains(t, doctorpkg.ReactNativeCheckNames, "xcelerate-wrapper-path")
	assert.Contains(t, doctorpkg.ReactNativeCheckNames, "ccache-helper")
}

func TestRNDoctor_HealthySetupIsSilentButDebugLogged(t *testing.T) {
	d, out, _ := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	})

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), buildOutcome{ChildInvocations: 2})

	assert.NotContains(t, out.String(), msgDoctorIssuesFound)
	assert.NotContains(t, out.String(), msgDoctorIssuesRecap)

	var debugOut strings.Builder
	d, _, _ = newTestDoctor(func(doctorpkg.Options) doctorpkg.Report { return okReport("auth") })
	d.Logger = log.NewLogger(log.WithOutput(&debugOut), log.WithDebugLog(true))
	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), buildOutcome{})

	assert.Contains(t, debugOut.String(), "Running health checks")
	assert.Contains(t, debugOut.String(), "Health check result: ok")
	assert.Contains(t, debugOut.String(), "Child invocations recorded during this build: 0")
}

func TestRNDoctor_EndOfBuildRepeatsStartIssuesWithoutRechecking(t *testing.T) {
	d, out, opts := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return warnReport("auth", "no credentials found")
	})

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), buildOutcome{})

	assert.Len(t, *opts, 1, "the recap must not re-run the checks")
	assert.Contains(t, out.String(), msgDoctorIssuesRecap)
	assert.Equal(t, 2, strings.Count(out.String(), "auth: no credentials found"))
}

func TestRNDoctor_SaveFailureProbeSupersedesTheStartReport(t *testing.T) {
	d, out, opts := newTestDoctor(func(o doctorpkg.Options) doctorpkg.Report {
		if o.SkipBackendProbe {
			return warnReport("auth", "no credentials found")
		}

		return warnReport("auth-backend", "token rejected by the backend")
	})

	d.CheckAtStart(context.Background())
	d.OnInvocationSaveFailure(context.Background())
	d.ReportAtEnd(context.Background(), buildOutcome{InvocationSaveFailed: true})

	require.Len(t, *opts, 2)
	assert.Equal(t, doctorpkg.AuthProbeCheckNames, (*opts)[1].Only)
	assert.False(t, (*opts)[1].SkipBackendProbe, "the probe is the point of this run")

	assert.Contains(t, out.String(), msgDoctorProbingAuth)
	assert.Contains(t, out.String(), "auth-backend: token rejected by the backend")
	assert.NotContains(t, out.String(), msgDoctorIssuesRecap, "the probe's verdict replaces the buffered report")
}

func TestRNDoctor_HealthyProbeClearsSuspicionOfAnExpiredToken(t *testing.T) {
	d, out, _ := newTestDoctor(func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth-backend")
	})

	d.OnInvocationSaveFailure(context.Background())

	assert.Contains(t, out.String(), msgDoctorProbeNoIssues)
	assert.NotContains(t, out.String(), msgDoctorIssuesFound)
}
