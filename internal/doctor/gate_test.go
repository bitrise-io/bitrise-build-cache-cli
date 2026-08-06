//go:build unit

package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gateWarnReport(name, detail string) Report {
	return Report{Items: []ReportItem{
		{Name: name, Result: Result{State: StateWarn, Detail: detail}},
	}}
}

func gateOKReport(name string) Report {
	return Report{Items: []ReportItem{
		{Name: name, Result: Result{State: StateOK, Detail: "fine"}},
	}}
}

func newTestGate(debug bool, report func(Options) Report) (*Gate, *strings.Builder, *[]Options) {
	var out strings.Builder
	opts := &[]Options{}

	g := &Gate{
		Logger: log.NewLogger(log.WithOutput(&out), log.WithDebugLog(debug)),
		RunChecks: func(_ context.Context, o Options) Report {
			*opts = append(*opts, o)

			return report(o)
		},
	}

	return g, &out, opts
}

func TestGate_StartCheckSkipsNetworkCallsAndFixes(t *testing.T) {
	g, out, opts := newTestGate(false, func(Options) Report {
		return gateWarnReport("auth", "no credentials found")
	})

	g.CheckAtStart(context.Background(), []string{"auth", "log-dirs"})

	require.Len(t, *opts, 1)
	assert.Equal(t, []string{"auth", "log-dirs"}, (*opts)[0].Only)
	assert.True(t, (*opts)[0].SkipBackendProbe, "no network round-trip per build")
	assert.True(t, (*opts)[0].SkipUpdateCheck)
	assert.False(t, (*opts)[0].ApplyFixes, "a build must not mutate the setup")

	assert.Contains(t, out.String(), MsgGateIssuesFound)
	assert.Contains(t, out.String(), "auth: no credentials found")
	assert.Contains(t, out.String(), MsgGateRepairHint)
}

func TestGate_HealthySetupIsSilentButDebugLogged(t *testing.T) {
	g, out, _ := newTestGate(false, func(Options) Report { return gateOKReport("auth") })

	g.CheckAtStart(context.Background(), []string{"auth"})
	g.Recap()

	assert.NotContains(t, out.String(), MsgGateIssuesFound)
	assert.NotContains(t, out.String(), MsgGateIssuesRecap)

	g, debugOut, _ := newTestGate(true, func(Options) Report { return gateOKReport("auth") })

	g.CheckAtStart(context.Background(), []string{"auth"})

	assert.Contains(t, debugOut.String(), "Running health checks")
	assert.Contains(t, debugOut.String(), "Health check result: ok")
}

func TestGate_RecapRepeatsStartIssuesWithoutRechecking(t *testing.T) {
	g, out, opts := newTestGate(false, func(Options) Report {
		return gateWarnReport("auth", "no credentials found")
	})

	g.CheckAtStart(context.Background(), []string{"auth"})
	g.Recap()

	assert.Len(t, *opts, 1, "the recap must not re-run the checks")
	assert.Contains(t, out.String(), MsgGateIssuesRecap)
	assert.Equal(t, 2, strings.Count(out.String(), "auth: no credentials found"))
}

func TestGate_AuthProbeSupersedesTheStartReport(t *testing.T) {
	g, out, opts := newTestGate(false, func(o Options) Report {
		if o.SkipBackendProbe {
			return gateWarnReport("auth", "no credentials found")
		}

		return gateWarnReport("auth-backend", "token rejected by the backend")
	})

	g.CheckAtStart(context.Background(), []string{"auth"})
	g.ProbeAuth(context.Background())
	g.Recap()

	require.Len(t, *opts, 2)
	assert.Equal(t, AuthProbeCheckNames, (*opts)[1].Only)
	assert.False(t, (*opts)[1].SkipBackendProbe, "the probe is the point of this run")

	assert.Contains(t, out.String(), MsgGateProbingAuth)
	assert.Contains(t, out.String(), "auth-backend: token rejected by the backend")
	assert.NotContains(t, out.String(), MsgGateIssuesRecap, "the probe's verdict replaces the buffered report")
}

func TestGate_HealthyProbeClearsSuspicionOfAnExpiredToken(t *testing.T) {
	g, out, _ := newTestGate(false, func(Options) Report { return gateOKReport("auth-backend") })

	g.ProbeAuth(context.Background())

	assert.Contains(t, out.String(), MsgGateProbeNoIssues)
	assert.NotContains(t, out.String(), MsgGateIssuesFound)
}
