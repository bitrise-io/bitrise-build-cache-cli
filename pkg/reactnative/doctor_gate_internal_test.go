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

func newTestDoctor(debug bool, report func(doctorpkg.Options) doctorpkg.Report) (*rnDoctor, *strings.Builder, *[]doctorpkg.Options) {
	var out strings.Builder
	opts := &[]doctorpkg.Options{}

	d := &rnDoctor{
		Logger: log.NewLogger(log.WithOutput(&out), log.WithDebugLog(debug)),
		RunChecks: func(_ context.Context, o doctorpkg.Options) doctorpkg.Report {
			*opts = append(*opts, o)

			return report(o)
		},
	}

	return d, &out, opts
}

func TestRNDoctor_ChecksTheReactNativeSetAndReportsItsIssues(t *testing.T) {
	d, out, opts := newTestDoctor(false, func(doctorpkg.Options) doctorpkg.Report {
		return doctorpkg.Report{Items: []doctorpkg.ReportItem{{
			Name:   "ccache-helper",
			Result: doctorpkg.Result{State: doctorpkg.StateWarn, Detail: "not running (no socket file)"},
		}}}
	})

	d.CheckAtStart(context.Background())

	require.Len(t, *opts, 1)
	assert.Equal(t, doctorpkg.ReactNativeCheckNames, (*opts)[0].Only)
	assert.Contains(t, out.String(), doctorpkg.MsgGateIssuesFound)
	assert.Contains(t, out.String(), "ccache-helper: not running (no socket file)")

	d.ReportAtEnd(context.Background(), buildOutcome{})
	assert.Len(t, *opts, 1, "the recap must not re-run the checks")
	assert.Contains(t, out.String(), doctorpkg.MsgGateIssuesRecap)
}

// RN drives all three build tools, but the proxy is started later by the nested
// xcodebuild wrapper, so a proxy that is down at this point is not a problem.
func TestRNDoctor_CheckSetExcludesTheProxy(t *testing.T) {
	assert.NotContains(t, doctorpkg.ReactNativeCheckNames, "xcelerate-proxy")
	assert.Contains(t, doctorpkg.ReactNativeCheckNames, "xcelerate-wrapper-path")
	assert.Contains(t, doctorpkg.ReactNativeCheckNames, "ccache-helper")
}

func TestRNDoctor_ChildInvocationCountIsDebugOnly(t *testing.T) {
	okReport := func(doctorpkg.Options) doctorpkg.Report {
		return doctorpkg.Report{Items: []doctorpkg.ReportItem{
			{Name: "auth", Result: doctorpkg.Result{State: doctorpkg.StateOK, Detail: "fine"}},
		}}
	}

	d, out, _ := newTestDoctor(false, okReport)
	d.ReportAtEnd(context.Background(), buildOutcome{})
	assert.NotContains(t, out.String(), "Child invocations")

	d, debugOut, _ := newTestDoctor(true, okReport)
	d.ReportAtEnd(context.Background(), buildOutcome{})
	assert.Contains(t, debugOut.String(), "Child invocations recorded during this build: 0")
}

func TestRNDoctor_SaveFailureRunsTheBackendProbe(t *testing.T) {
	d, out, opts := newTestDoctor(false, func(doctorpkg.Options) doctorpkg.Report {
		return doctorpkg.Report{Items: []doctorpkg.ReportItem{{
			Name:   "auth-backend",
			Result: doctorpkg.Result{State: doctorpkg.StateWarn, Detail: "token rejected by the backend"},
		}}}
	})

	d.OnInvocationSaveFailure(context.Background())

	require.Len(t, *opts, 1)
	assert.Equal(t, doctorpkg.AuthProbeCheckNames, (*opts)[0].Only)
	assert.False(t, (*opts)[0].SkipBackendProbe, "the probe is the point of this run")
	assert.Contains(t, out.String(), "auth-backend: token rejected by the backend")
}
