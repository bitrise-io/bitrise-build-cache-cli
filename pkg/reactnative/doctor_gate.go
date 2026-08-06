package reactnative

import (
	"context"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
)

const (
	doctorLocalTimeout = 5 * time.Second
	doctorProbeTimeout = 15 * time.Second

	msgDoctorIssuesFound   = "Bitrise Build Cache health check found issues that can affect this build:"
	msgDoctorRepairHint    = "Run `bitrise-build-cache doctor --fix --interactive` to repair."
	msgDoctorIssuesRecap   = "Reminder — the health check at the start of this build reported:"
	msgDoctorProbingAuth   = "Checking whether an expired auth token caused the failure above..."
	msgDoctorProbeNoIssues = "Auth looks healthy, so the failure above is not a token problem."
)

//go:generate moq -stub -out doctor_gate_mock_test.go -pkg reactnative . buildHealthReporter

type buildHealthReporter interface {
	CheckAtStart(ctx context.Context)
	ReportAtEnd(ctx context.Context, outcome buildOutcome)
	OnInvocationSaveFailure(ctx context.Context)
}

// buildOutcome is what only the finished run knows.
type buildOutcome struct {
	// ChildInvocations is how many nested build-tool invocations reported to the
	// child stats ledger.
	ChildInvocations int
	// InvocationSaveFailed marks the analytics PUT an expired token would fail.
	InvocationSaveFailed bool
}

// rnDoctor runs the React-Native-relevant subset of the doctor checks around a
// wrapped command.
type rnDoctor struct {
	Logger    log.Logger
	Debug     bool
	RunChecks func(ctx context.Context, opts doctorpkg.Options) doctorpkg.Report

	startIssues []string
	// probeReported keeps the end-of-build recap from repeating the probe's report.
	probeReported bool
}

// CheckAtStart reports the issues that could degrade or break this build. The
// backend probe is deliberately left out — a network round-trip per build is too
// much overhead; OnInvocationSaveFailure covers the expired-token case instead.
func (d *rnDoctor) CheckAtStart(ctx context.Context) {
	report := d.run(ctx, doctorLocalTimeout, doctorpkg.Options{
		Only:             doctorpkg.ReactNativeCheckNames,
		SkipUpdateCheck:  true,
		SkipBackendProbe: true,
	})

	d.startIssues = doctorpkg.IssueLines(report)
	if len(d.startIssues) == 0 {
		return
	}

	d.print(msgDoctorIssuesFound, d.startIssues)
}

// ReportAtEnd repeats the start-of-build issues, a whole build's output back by
// now, and reports what no setup check can see.
func (d *rnDoctor) ReportAtEnd(_ context.Context, outcome buildOutcome) {
	// Zero children means the build tools the wrapped command ran never went
	// through the Bitrise wrappers — but a wrapped command that isn't a build
	// (`npm install`, `expo prebuild`) has none either, so this can't be a
	// warning without knowing which was which.
	d.Logger.Debugf("Child invocations recorded during this build: %d", outcome.ChildInvocations)

	switch {
	case d.probeReported:
		d.Logger.Debugf("Health issues were reported by the auth check above; not repeating them")
	case len(d.startIssues) == 0:
		d.Logger.Debugf("Health check found no issues at the start of this build")
	default:
		d.print(msgDoctorIssuesRecap, d.startIssues)
	}
}

// OnInvocationSaveFailure runs the backend probe an expired token would fail, so
// its verdict supersedes the buffered start-of-build report.
func (d *rnDoctor) OnInvocationSaveFailure(ctx context.Context) {
	d.startIssues, d.probeReported = nil, true

	d.Logger.TInfof(msgDoctorProbingAuth)

	report := d.run(ctx, doctorProbeTimeout, doctorpkg.Options{
		Only:            doctorpkg.AuthProbeCheckNames,
		SkipUpdateCheck: true,
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
func (d *rnDoctor) run(ctx context.Context, timeout time.Duration, opts doctorpkg.Options) doctorpkg.Report {
	d.Logger.Debugf("Running health checks %v (backend probe skipped: %t)", opts.Only, opts.SkipBackendProbe)

	report := d.diagnose(ctx, timeout, opts)

	for _, l := range doctorpkg.Lines(report.Items) {
		d.Logger.Debugf("  %s", l)
	}
	d.Logger.Debugf("Health check result: %s", doctorpkg.EffectiveOverall(report))

	return report
}

func (d *rnDoctor) diagnose(ctx context.Context, timeout time.Duration, opts doctorpkg.Options) doctorpkg.Report {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if d.RunChecks != nil {
		return d.RunChecks(runCtx, opts)
	}

	doc := doctorpkg.NewDoctor()
	doc.Debug = d.Debug

	return doc.Run(runCtx, opts)
}

func (d *rnDoctor) print(header string, lines []string) {
	d.Logger.Warnf(header)
	for _, l := range lines {
		d.Logger.Warnf("  %s", l)
	}
	d.Logger.Warnf(msgDoctorRepairHint)
}
