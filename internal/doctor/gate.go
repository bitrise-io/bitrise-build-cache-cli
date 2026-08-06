package doctor

import (
	"context"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	gateLocalTimeout = 5 * time.Second
	gateProbeTimeout = 15 * time.Second

	MsgGateIssuesFound   = "Bitrise Build Cache health check found issues that can affect this build:"
	MsgGateRepairHint    = "Run `bitrise-build-cache doctor --fix --interactive` to repair."
	MsgGateIssuesRecap   = "Reminder — the health check at the start of this build reported:"
	MsgGateProbingAuth   = "Checking whether an expired auth token caused the failure above..."
	MsgGateProbeNoIssues = "Auth looks healthy, so the failure above is not a token problem."
)

// Gate runs a subset of the checks around a build, reporting through a logger
// rather than the doctor command's table. Each build wrapper wraps one, adding
// its own check set and whatever end-of-build reporting only it can do.
type Gate struct {
	Logger log.Logger
	Debug  bool
	// RunChecks replaces the real check run in tests.
	RunChecks func(ctx context.Context, opts Options) Report

	startIssues []string
	// probeReported keeps Recap from repeating what ProbeAuth reported.
	probeReported bool
}

// CheckAtStart reports the issues that could degrade or break this build. The
// backend probe is deliberately left out — a network round-trip per build is too
// much overhead; ProbeAuth covers the expired-token case instead.
func (g *Gate) CheckAtStart(ctx context.Context, names []string) {
	report := g.run(ctx, gateLocalTimeout, Options{
		Only:             names,
		SkipUpdateCheck:  true,
		SkipBackendProbe: true,
	})

	g.startIssues = IssueLines(report)
	if len(g.startIssues) == 0 {
		return
	}

	g.print(MsgGateIssuesFound, g.startIssues)
}

// Recap repeats the start-of-build issues, a whole build's output back by now.
func (g *Gate) Recap() {
	switch {
	case g.probeReported:
		g.Logger.Debugf("Health issues were reported by the auth check above; not repeating them")
	case len(g.startIssues) == 0:
		g.Logger.Debugf("Health check found no issues at the start of this build")
	default:
		g.print(MsgGateIssuesRecap, g.startIssues)
	}
}

// ProbeAuth runs the backend probe an expired token would fail, so its verdict
// supersedes the buffered start-of-build report.
func (g *Gate) ProbeAuth(ctx context.Context) {
	g.startIssues, g.probeReported = nil, true

	g.Logger.TInfof(MsgGateProbingAuth)

	report := g.run(ctx, gateProbeTimeout, Options{
		Only:            AuthProbeCheckNames,
		SkipUpdateCheck: true,
	})

	issues := IssueLines(report)
	if len(issues) == 0 {
		g.Logger.TInfof(MsgGateProbeNoIssues)

		return
	}

	g.print(MsgGateIssuesFound, issues)
}

// run always debug-logs the full report, so a build log shows the checks ran
// even when they all passed.
func (g *Gate) run(ctx context.Context, timeout time.Duration, opts Options) Report {
	g.Logger.Debugf("Running health checks %v (backend probe skipped: %t)", opts.Only, opts.SkipBackendProbe)

	report := g.diagnose(ctx, timeout, opts)

	for _, l := range Lines(report.Items) {
		g.Logger.Debugf("  %s", l)
	}
	g.Logger.Debugf("Health check result: %s", EffectiveOverall(report))

	return report
}

func (g *Gate) diagnose(ctx context.Context, timeout time.Duration, opts Options) Report {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if g.RunChecks != nil {
		return g.RunChecks(runCtx, opts)
	}

	doc := NewDoctor()
	doc.Debug = g.Debug

	return doc.Run(runCtx, opts)
}

func (g *Gate) print(header string, lines []string) {
	g.Logger.Warnf(header)
	for _, l := range lines {
		g.Logger.Warnf("  %s", l)
	}
	g.Logger.Warnf(MsgGateRepairHint)
}
