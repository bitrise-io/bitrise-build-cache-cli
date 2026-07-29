package xcode

import (
	"context"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
)

const (
	// NoDoctorFlag skips the wrapper's health check for one invocation.
	NoDoctorFlag = "--no-doctor"
	// EnvSkipDoctor skips the wrapper's health check for every invocation.
	EnvSkipDoctor = "BITRISE_BUILD_CACHE_SKIP_DOCTOR"

	doctorLocalTimeout = 5 * time.Second
	doctorProbeTimeout = 15 * time.Second

	msgDoctorIssuesFound   = "Bitrise Build Cache health check found issues that can affect this build:"
	msgDoctorRepairHint    = "Run `bitrise-build-cache doctor --fix` to repair."
	msgDoctorIssuesRecap   = "Reminder — the health check at the start of this build reported:"
	msgDoctorProbingAuth   = "Checking whether an expired auth token caused the failure above..."
	msgDoctorProbeNoIssues = "Auth looks healthy, so the failure above is not a token problem."
	msgDoctorCASErrors     = "The compilation cache reported %d error(s) during this build — those lookups never completed, so the files compiled locally instead."
	msgDoctorCASErrorsHint = "Check `bitrise-build-cache doctor` and the proxy log; `xcelerate stop-proxy` makes the next build start a fresh proxy."
)

//go:generate moq -stub -out doctor_gate_mock_test.go -pkg xcode . buildHealthReporter

// buildHealthReporter surfaces local setup problems around an xcodebuild run.
type buildHealthReporter interface {
	CheckAtStart(ctx context.Context)
	ReportAtEnd(ctx context.Context, stats xcodeargs.CompCacheStats)
	OnInvocationSaveFailure(ctx context.Context)
}

// xcodeDoctor runs the Xcode-relevant subset of the doctor checks around a build.
type xcodeDoctor struct {
	Logger log.Logger
	Debug  bool
	// CacheEnabled selects the check set: with the cache off there's no proxy to
	// report on, but auth still gates the analytics PUT.
	CacheEnabled bool
	// AuthConfig is the credential this invocation's analytics PUT uses. The
	// save-failure probe tests exactly this one, so it can't come back healthy on
	// a different credential the machine happens to have.
	AuthConfig configcommon.CacheAuthConfig
	// RunChecks defaults to a real doctor run; injected in tests.
	RunChecks func(ctx context.Context, opts doctorpkg.Options) doctorpkg.Report

	startIssues []string
}

func newXcodeDoctor(logger log.Logger, debug, cacheEnabled bool, authConfig configcommon.CacheAuthConfig) *xcodeDoctor {
	return &xcodeDoctor{Logger: logger, Debug: debug, CacheEnabled: cacheEnabled, AuthConfig: authConfig}
}

func (d *xcodeDoctor) startCheckNames() []string {
	if d.CacheEnabled {
		return doctorpkg.XcodeCheckNames
	}

	return doctorpkg.XcodeAnalyticsOnlyCheckNames
}

// CheckAtStart reports the issues that could degrade or break this build. The
// backend probe is deliberately left out — a network round-trip per build is too
// much overhead; OnInvocationSaveFailure covers the expired-token case instead.
func (d *xcodeDoctor) CheckAtStart(ctx context.Context) {
	report := d.run(ctx, doctorLocalTimeout, doctorpkg.Options{
		Only:             d.startCheckNames(),
		SkipUpdateCheck:  true,
		SkipBackendProbe: true,
	})

	d.startIssues = doctorpkg.IssueLines(report)
	if len(d.startIssues) == 0 {
		return
	}

	d.print(msgDoctorIssuesFound, d.startIssues)
}

// ReportAtEnd repeats the start-of-build issues, which by now are thousands of
// xcodebuild log lines back, and reports cache errors no setup check can see.
func (d *xcodeDoctor) ReportAtEnd(_ context.Context, stats xcodeargs.CompCacheStats) {
	if stats.CASErrors > 0 {
		d.Logger.Warnf(msgDoctorCASErrors, stats.CASErrors)
		d.Logger.Warnf(msgDoctorCASErrorsHint)
	}

	if len(d.startIssues) == 0 {
		d.Logger.Debugf("No health-check issues to repeat at the end of this build")

		return
	}

	d.print(msgDoctorIssuesRecap, d.startIssues)
}

// OnInvocationSaveFailure diagnoses a failed analytics PUT, which an expired
// token would explain. This runs the backend probe, so its verdict supersedes
// the buffered start-of-build report.
func (d *xcodeDoctor) OnInvocationSaveFailure(ctx context.Context) {
	d.startIssues = nil

	d.Logger.TInfof(msgDoctorProbingAuth)

	report := d.run(ctx, doctorProbeTimeout, doctorpkg.Options{
		Only:            doctorpkg.AuthProbeCheckNames,
		SkipUpdateCheck: true,
		PinAuth:         &d.AuthConfig,
	})

	issues := doctorpkg.IssueLines(report)
	if len(issues) == 0 {
		d.Logger.TInfof(msgDoctorProbeNoIssues)

		return
	}

	d.print(msgDoctorIssuesFound, issues)
}

// run diagnoses and always debug-logs the full report, so a build log shows the
// checks ran even when they all passed.
func (d *xcodeDoctor) run(ctx context.Context, timeout time.Duration, opts doctorpkg.Options) doctorpkg.Report {
	d.Logger.Debugf("Running health checks %v (backend probe skipped: %t)", opts.Only, opts.SkipBackendProbe)

	report := d.diagnose(ctx, timeout, opts)

	for _, l := range doctorpkg.Lines(report.Items) {
		d.Logger.Debugf("  %s", l)
	}
	d.Logger.Debugf("Health check result: %s", doctorpkg.EffectiveOverall(report))

	return report
}

func (d *xcodeDoctor) diagnose(ctx context.Context, timeout time.Duration, opts doctorpkg.Options) doctorpkg.Report {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if d.RunChecks != nil {
		return d.RunChecks(runCtx, opts)
	}

	doc := doctorpkg.NewDoctor()
	doc.Debug = d.Debug
	doc.AuthOverride = opts.PinAuth

	return doc.Run(runCtx, opts)
}

func (d *xcodeDoctor) print(header string, lines []string) {
	d.Logger.Warnf(header)
	for _, l := range lines {
		d.Logger.Warnf("  %s", l)
	}
	d.Logger.Warnf(msgDoctorRepairHint)
}
