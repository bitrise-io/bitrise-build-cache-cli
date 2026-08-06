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
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
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

func tempHomeProxy(t *testing.T) (utils.OsProxy, string) {
	t.Helper()

	home := t.TempDir()

	return &utilsMocks.OsProxyMock{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		MkdirAllFunc:    func(name string, mode os.FileMode) error { return os.MkdirAll(name, mode) },
	}, home
}

func proxyLogDir(t *testing.T, home string) string {
	t.Helper()

	dir := filepath.Join(home, ".local", "state", "xcelerate", "logs")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	return dir
}

func newTestDoctor(t *testing.T, report func(doctorpkg.Options) doctorpkg.Report) (*xcodeDoctor, *strings.Builder, *[]doctorpkg.Options) {
	t.Helper()

	var out strings.Builder
	opts := &[]doctorpkg.Options{}

	osProxy, _ := tempHomeProxy(t)
	d := &xcodeDoctor{
		Logger:       log.NewLogger(log.WithOutput(&out)),
		OsProxy:      osProxy,
		CacheEnabled: true,
		RunChecks: func(_ context.Context, o doctorpkg.Options) doctorpkg.Report {
			*opts = append(*opts, o)

			return report(o)
		},
	}

	return d, &out, opts
}

func TestXcodeDoctor_StartCheckSkipsNetworkCalls(t *testing.T) {
	d, out, opts := newTestDoctor(t, func(doctorpkg.Options) doctorpkg.Report {
		return warnReport("xcelerate-proxy", "not running (no socket file)")
	})

	d.CheckAtStart(context.Background())

	require.Len(t, *opts, 1)
	assert.Equal(t, doctorpkg.XcodeCheckNames, (*opts)[0].Only)
	assert.True(t, (*opts)[0].SkipBackendProbe, "no network round-trip per build")
	assert.True(t, (*opts)[0].SkipUpdateCheck)
	assert.False(t, (*opts)[0].ApplyFixes, "a build must not mutate the setup")

	assert.Contains(t, out.String(), doctorpkg.MsgGateIssuesFound)
	assert.Contains(t, out.String(), "xcelerate-proxy: not running (no socket file)")
}

func TestXcodeDoctor_CacheOffDropsProxyCheckButKeepsAuth(t *testing.T) {
	var out strings.Builder
	var opts []doctorpkg.Options

	osProxy, _ := tempHomeProxy(t)
	d := &xcodeDoctor{Logger: log.NewLogger(log.WithOutput(&out)), OsProxy: osProxy}
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
	d, out, _ := newTestDoctor(t, func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	})

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), buildOutcome{})

	assert.Empty(t, out.String())
}

func TestXcodeDoctor_HealthySetupLogsAtDebug(t *testing.T) {
	var out strings.Builder
	logger := log.NewLogger(log.WithOutput(&out))
	logger.EnableDebugLog(true)

	osProxy, _ := tempHomeProxy(t)
	d := &xcodeDoctor{Logger: logger, OsProxy: osProxy, CacheEnabled: true}
	d.RunChecks = func(context.Context, doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	}

	d.CheckAtStart(context.Background())
	d.ReportAtEnd(context.Background(), buildOutcome{})

	logged := out.String()
	assert.Contains(t, logged, "Running health checks")
	assert.Contains(t, logged, "ok auth: fine")
	assert.Contains(t, logged, "Health check result: ok")
	assert.Contains(t, logged, "found no issues at the start")
}

func TestXcodeDoctor_ReportAtEndRepeatsStartIssues(t *testing.T) {
	d, out, opts := newTestDoctor(t, func(doctorpkg.Options) doctorpkg.Report {
		return warnReport("auth", "token expired")
	})

	d.CheckAtStart(context.Background())
	out.Reset()
	d.ReportAtEnd(context.Background(), buildOutcome{})

	assert.Len(t, *opts, 1, "the end-of-build recap must not re-run the checks")
	assert.Contains(t, out.String(), doctorpkg.MsgGateIssuesRecap)
	assert.Contains(t, out.String(), "auth: token expired")
}

func TestXcodeDoctor_SaveFailureProbesBackendAndDropsStartBuffer(t *testing.T) {
	d, out, opts := newTestDoctor(t, func(o doctorpkg.Options) doctorpkg.Report {
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

	assert.Contains(t, out.String(), doctorpkg.MsgGateProbingAuth)
	assert.Contains(t, out.String(), "auth-backend: unauthorized (token expired)")

	out.Reset()
	d.ReportAtEnd(context.Background(), buildOutcome{})
	assert.NotContains(t, out.String(), doctorpkg.MsgGateIssuesRecap,
		"the probe verdict supersedes the start-of-build report")
	assert.NotContains(t, out.String(), "found no issues",
		"issues were found — the recap must not claim otherwise")
}

// One number, whichever side saw more: the same failure is usually counted on
// both, and the compiler's count is what the build actually felt.
func TestXcodeDoctor_ReportAtEndShowsTheLargerErrorCount(t *testing.T) {
	cases := map[string]struct {
		outcome buildOutcome
		want    string
	}{
		"compiler saw more": {
			outcome: buildOutcome{
				CAS:   xcodeargs.CompCacheStats{CASErrors: 4002},
				Proxy: proxyOutcome{Errors: 12},
			},
			want: "4002 error(s)",
		},
		"only the proxy saw them (a failed upload the compiler ignores)": {
			outcome: buildOutcome{Proxy: proxyOutcome{Errors: 12}},
			want:    "12 error(s)",
		},
		"only the compiler saw them (never reached the proxy)": {
			outcome: buildOutcome{CAS: xcodeargs.CompCacheStats{CASErrors: 7}},
			want:    "7 error(s)",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d, out, _ := newTestDoctor(t, func(doctorpkg.Options) doctorpkg.Report {
				return okReport("auth")
			})

			d.CheckAtStart(context.Background())
			out.Reset()
			d.ReportAtEnd(context.Background(), tc.outcome)

			assert.Contains(t, out.String(), tc.want)
			assert.Contains(t, out.String(), "bitrise-build-cache doctor")
		})
	}
}

// 0% on a first build is normal, not a symptom.
func TestXcodeDoctor_ReportAtEndSilentOnColdCacheWithoutErrors(t *testing.T) {
	d, out, _ := newTestDoctor(t, func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth")
	})

	d.CheckAtStart(context.Background())
	out.Reset()
	d.ReportAtEnd(context.Background(), buildOutcome{CAS: xcodeargs.CompCacheStats{Hits: 0, TotalTasks: 2746, CASErrors: 0}})

	assert.Empty(t, out.String())
}

func TestXcodeDoctor_SaveFailureWithHealthyAuth(t *testing.T) {
	d, out, _ := newTestDoctor(t, func(doctorpkg.Options) doctorpkg.Report {
		return okReport("auth-backend")
	})

	d.OnInvocationSaveFailure(context.Background())

	assert.Contains(t, out.String(), doctorpkg.MsgGateProbeNoIssues)
	assert.NotContains(t, out.String(), doctorpkg.MsgGateIssuesFound)
}
