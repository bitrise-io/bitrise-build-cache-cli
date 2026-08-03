package xcode

import (
	"context"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
)

const (
	// Opt-outs: the flag for one invocation, the env var for every one.
	NoDoctorFlag  = "--no-doctor"
	EnvSkipDoctor = "BITRISE_BUILD_CACHE_SKIP_DOCTOR"

	doctorLocalTimeout = 5 * time.Second
	doctorProbeTimeout = 15 * time.Second

	msgDoctorIssuesFound      = "Bitrise Build Cache health check found issues that can affect this build:"
	msgDoctorRepairHint       = "Run `bitrise-build-cache doctor --fix --interactive` to repair."
	msgDoctorIssuesRecap      = "Reminder — the health check at the start of this build reported:"
	msgDoctorProbingAuth      = "Checking whether an expired auth token caused the failure above..."
	msgDoctorProbeNoIssues    = "Auth looks healthy, so the failure above is not a token problem."
	msgDoctorCASErrors        = "The compilation cache reported %d error(s) during this build — those lookups never completed, so the files compiled locally instead."
	msgDoctorCASOnlyErrors    = "Xcode reported %d compilation-cache error(s), but the proxy completed every request it received — so those lookups are not reaching the proxy."
	msgDoctorCASOnlyHint      = "Check that the socket the compiler was given still exists: `bitrise-build-cache doctor`, then `activate xcode` to rewrite it."
	msgDoctorCacheErrorsHint  = "Check `bitrise-build-cache doctor`; `xcelerate stop-proxy` makes the next build start a fresh proxy."
	msgDoctorProxyErrors      = "The Bitrise compilation cache proxy could not complete %d request(s) during this build:"
	msgDoctorProxyUnreachable = "The Bitrise compilation cache proxy stopped answering during this build, so nothing was cached from that point on."
	msgDoctorProxyStderr      = "What the proxy wrote to its error log during this build:"
	msgDoctorProxyLogHint     = "Full proxy log: %s"
)

//go:generate moq -stub -out doctor_gate_mock_test.go -pkg xcode . buildHealthReporter

type buildHealthReporter interface {
	CheckAtStart(ctx context.Context)
	ReportAtEnd(ctx context.Context, outcome buildOutcome)
	OnInvocationSaveFailure(ctx context.Context)
}

type buildOutcome struct {
	CAS   xcodeargs.CompCacheStats
	Proxy proxyOutcome
}

type proxyOutcome struct {
	Errors     int64
	FirstError string
	// Unreachable: the stats call failed, so the counts above say nothing.
	Unreachable bool
}

// xcodeDoctor runs the Xcode-relevant subset of the doctor checks around a build.
type xcodeDoctor struct {
	Logger       log.Logger
	Debug        bool
	CacheEnabled bool
	// AuthConfig must be the credential this invocation's PUT used, so the
	// save-failure probe can't pass on a different one the machine can resolve.
	AuthConfig   configcommon.CacheAuthConfig
	InvocationID string
	OsProxy      utils.OsProxy
	RunChecks    func(ctx context.Context, opts doctorpkg.Options) doctorpkg.Report

	startIssues []string
	// proxyErrOffset is the shared error log's size at build start; past it is ours.
	proxyErrOffset int64
	// probeReported keeps the end-of-build recap from repeating the probe's report.
	probeReported bool
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

	d.markProxyErrLog()

	d.startIssues = doctorpkg.IssueLines(report)
	if len(d.startIssues) == 0 {
		return
	}

	d.print(msgDoctorIssuesFound, d.startIssues)
}

// ReportAtEnd repeats the start-of-build issues, thousands of xcodebuild log
// lines back by now, and reports what no setup check can see.
func (d *xcodeDoctor) ReportAtEnd(_ context.Context, outcome buildOutcome) {
	if d.reportCacheErrors(outcome) {
		d.Logger.Warnf(msgDoctorCacheErrorsHint)
	}

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
func (d *xcodeDoctor) OnInvocationSaveFailure(ctx context.Context) {
	d.startIssues, d.probeReported = nil, true

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

func (d *xcodeDoctor) osProxy() utils.OsProxy {
	if d.OsProxy != nil {
		return d.OsProxy
	}

	return utils.DefaultOsProxy{}
}

func (d *xcodeDoctor) markProxyErrLog() {
	path, err := getProxyErrorLogFile(d.osProxy())
	if err != nil {
		d.Logger.Debugf("Could not resolve the proxy error log: %s", err)

		return
	}

	d.proxyErrOffset = fileSize(path)
}

// reportCacheErrors reconciles the two views of a failed lookup. The proxy counts
// requests it could not complete; Xcode's output counts what the compiler saw. A
// count on only one side is the interesting case: the compiler reporting errors
// the proxy never received means the two aren't talking, which neither number
// says on its own.
func (d *xcodeDoctor) reportCacheErrors(outcome buildOutcome) bool {
	proxyReported := d.reportProxy(outcome.Proxy)

	casErrors := outcome.CAS.CASErrors
	switch {
	case casErrors == 0:
		return proxyReported
	case proxyReported:
		// The proxy already gave the reason; the compiler's count is the same
		// failures seen from the other end.
		d.Logger.Warnf(msgDoctorCASErrors, casErrors)
	default:
		d.Logger.Warnf(msgDoctorCASOnlyErrors, casErrors)
		d.Logger.Warnf(msgDoctorCASOnlyHint)

		// A fresh proxy doesn't help when the compiler isn't reaching this one.
		return false
	}

	return true
}

// reportProxy relays what the proxy counted, falling back to its error log only
// when the proxy couldn't be asked — a dead one has no counters to report.
func (d *xcodeDoctor) reportProxy(proxy proxyOutcome) bool {
	switch {
	case proxy.Unreachable:
		d.Logger.Warnf(msgDoctorProxyUnreachable)
		d.reportProxyStderr()

		return true
	case proxy.Errors > 0:
		d.Logger.Warnf(msgDoctorProxyErrors, proxy.Errors)
		if proxy.FirstError != "" {
			d.Logger.Warnf("  first failure: %s", proxy.FirstError)
		}
		if path, err := getProxyLogFile(d.osProxy(), d.InvocationID); err == nil {
			d.Logger.Warnf(msgDoctorProxyLogHint, path)
		}

		return true
	}

	d.Logger.Debugf("Proxy completed every request for invocation %s", d.InvocationID)

	return false
}

func (d *xcodeDoctor) reportProxyStderr() {
	path, err := getProxyErrorLogFile(d.osProxy())
	if err != nil {
		return
	}

	stderr := readProxyStderrSince(path, d.proxyErrOffset)
	if stderr == "" {
		return
	}

	d.Logger.Warnf(msgDoctorProxyStderr)
	for _, l := range strings.Split(stderr, "\n") {
		d.Logger.Warnf("  %s", truncate(l, proxyErrorSnippetMax))
	}
}

func (d *xcodeDoctor) print(header string, lines []string) {
	d.Logger.Warnf(header)
	for _, l := range lines {
		d.Logger.Warnf("  %s", l)
	}
	d.Logger.Warnf(msgDoctorRepairHint)
}
