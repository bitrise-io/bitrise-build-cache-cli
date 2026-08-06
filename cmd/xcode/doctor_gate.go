package xcode

import (
	"context"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
)

const (
	NoDoctorFlag  = doctorpkg.NoDoctorFlag
	EnvSkipDoctor = doctorpkg.EnvSkipDoctor

	msgDoctorCacheErrors     = "The Bitrise cache reported %d error(s) during this build, so some files compiled locally instead."
	msgDoctorCacheStopped    = "The Bitrise cache stopped responding during this build, so nothing was cached after that."
	msgDoctorCacheErrorsHint = "Run `bitrise-build-cache doctor` to check the setup."
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
	InvocationID string
	OsProxy      utils.OsProxy
	RunChecks    func(ctx context.Context, opts doctorpkg.Options) doctorpkg.Report

	gate *doctorpkg.Gate
	// proxyErrOffset is the shared error log's size at build start; past it is ours.
	proxyErrOffset int64
}

func (d *xcodeDoctor) startCheckNames() []string {
	if d.CacheEnabled {
		return doctorpkg.XcodeCheckNames
	}

	return doctorpkg.XcodeAnalyticsOnlyCheckNames
}

func (d *xcodeDoctor) CheckAtStart(ctx context.Context) {
	d.doctorGate().CheckAtStart(ctx, d.startCheckNames())
	d.markProxyErrLog()
}

// ReportAtEnd reports what no setup check can see, then repeats the
// start-of-build issues, thousands of xcodebuild log lines back by now.
func (d *xcodeDoctor) ReportAtEnd(_ context.Context, outcome buildOutcome) {
	if d.reportCacheErrors(outcome) {
		d.Logger.Warnf(msgDoctorCacheErrorsHint)
	}

	d.doctorGate().Recap()
}

func (d *xcodeDoctor) OnInvocationSaveFailure(ctx context.Context) {
	d.doctorGate().ProbeAuth(ctx)
}

func (d *xcodeDoctor) doctorGate() *doctorpkg.Gate {
	if d.gate == nil {
		d.gate = &doctorpkg.Gate{Logger: d.Logger, Debug: d.Debug, RunChecks: d.RunChecks}
	}

	return d.gate
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
	d.logCacheErrorDetail(outcome)

	if outcome.Proxy.Unreachable {
		d.Logger.Warnf(msgDoctorCacheStopped)

		return true
	}

	// The larger of the two, never their sum: the same failure is usually counted
	// on both sides, and the compiler's count is what the build actually felt.
	if n := max(outcome.CAS.CASErrors, outcome.Proxy.Errors); n > 0 {
		d.Logger.Warnf(msgDoctorCacheErrors, n)

		return true
	}

	return false
}

// logCacheErrorDetail keeps which side saw what, and the proxy's own words, out of
// the build's output — a developer needs to know the cache misbehaved and what to
// run, not how the pieces talk to each other.
func (d *xcodeDoctor) logCacheErrorDetail(outcome buildOutcome) {
	d.Logger.Debugf("Cache errors — compiler saw %d, proxy counted %d, proxy unreachable: %t",
		outcome.CAS.CASErrors, outcome.Proxy.Errors, outcome.Proxy.Unreachable)

	if outcome.Proxy.FirstError != "" {
		d.Logger.Debugf("First proxy failure: %s", outcome.Proxy.FirstError)
	}

	if d.InvocationID != "" {
		if path, err := getProxyLogFile(d.osProxy(), d.InvocationID); err == nil {
			d.Logger.Debugf("Proxy log: %s", path)
		}
	}

	if path, err := getProxyErrorLogFile(d.osProxy()); err == nil {
		for _, l := range strings.Split(readProxyStderrSince(path, d.proxyErrOffset), "\n") {
			if l != "" {
				d.Logger.Debugf("Proxy stderr: %s", truncate(l, proxyErrorSnippetMax))
			}
		}
	}
}
