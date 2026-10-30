package xcelerate

import (
	"strings"
	"time"
)

// EnrichmentOptions declares which xcprobe collectors run and under what
// bounds. Zero value = all collectors off. Precedence: CLI > env.
type EnrichmentOptions struct {
	HostSnapshot       bool
	BuildTimingSummary bool
	BuildSettings      bool

	// Reserved slots: not yet instantiated by the aggregator but kept in the
	// struct so the loader / precedence shape stays stable.
	ActivityLog       bool
	ActivityLogAttach bool

	SwiftFunctionTimes bool
	SwiftTypeChecks    bool
	SwiftStats         bool
	ClangTimeTrace     bool
	LinkerStats        bool

	BuildDB bool

	SwiftTopN  int
	ClangTopN  int
	TargetTopN int
	StepTopN   int

	PerCollectorTimeout time.Duration
	FailOpen            bool
}

// AnyEnabled is the hook-site short-circuit — false means zero runtime cost.
func (o EnrichmentOptions) AnyEnabled() bool {
	return o.HostSnapshot ||
		o.BuildTimingSummary ||
		o.BuildSettings ||
		o.ActivityLog ||
		o.ActivityLogAttach ||
		o.SwiftFunctionTimes ||
		o.SwiftTypeChecks ||
		o.SwiftStats ||
		o.ClangTimeTrace ||
		o.LinkerStats ||
		o.BuildDB
}

// EnrichmentFlags carries --enrich / --enrich-all / --enrich-none values.
type EnrichmentFlags struct {
	Enabled []string
	All     bool
	None    bool
}

const (
	// EnvEnrichmentCollectors is a comma-separated collector list, e.g.
	// BITRISE_BUILD_CACHE_ENRICH=hostsnap,buildtiming,buildsettings
	EnvEnrichmentCollectors = "BITRISE_BUILD_CACHE_ENRICH"
	EnvEnrichmentAll        = "BITRISE_BUILD_CACHE_ENRICH_ALL"
	EnvEnrichmentNone       = "BITRISE_BUILD_CACHE_ENRICH_NONE"

	collectorHostSnap          = "hostsnap"
	collectorBuildTiming       = "buildtiming"
	collectorBuildSettings     = "buildsettings"
	collectorActivityLog       = "activitylog"
	collectorActivityLogAttach = "activitylog-attach"
	collectorAll               = "all"
	collectorNone              = "none"
)

// LoadEnrichmentOptions resolves EnrichmentOptions from env then CLI flags
// (flags win). None > All > per-collector list at each layer. A nil env is
// treated as returning "".
func LoadEnrichmentOptions(env func(string) string, flags EnrichmentFlags) EnrichmentOptions {
	if env == nil {
		env = func(string) string { return "" }
	}

	opts := EnrichmentOptions{
		PerCollectorTimeout: 10 * time.Second,
		FailOpen:            true,
	}

	applyCollectorList(&opts, parseCollectorList(env(EnvEnrichmentCollectors)))

	if truthy(env(EnvEnrichmentAll)) {
		setAllCollectors(&opts, true)
	}

	if truthy(env(EnvEnrichmentNone)) {
		setAllCollectors(&opts, false)
	}

	applyCollectorList(&opts, flags.Enabled)

	if flags.All {
		setAllCollectors(&opts, true)
	}

	if flags.None {
		setAllCollectors(&opts, false)
	}

	return opts
}

func parseCollectorList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}

		out = append(out, p)
	}

	return out
}

func applyCollectorList(opts *EnrichmentOptions, list []string) {
	for _, name := range list {
		switch name {
		case collectorHostSnap:
			opts.HostSnapshot = true
		case collectorBuildTiming:
			opts.BuildTimingSummary = true
		case collectorBuildSettings:
			opts.BuildSettings = true
		case collectorActivityLog:
			opts.ActivityLog = true
		case collectorActivityLogAttach:
			opts.ActivityLog = true
			opts.ActivityLogAttach = true
		case collectorAll:
			setAllCollectors(opts, true)
		case collectorNone:
			setAllCollectors(opts, false)
		}
	}
}

// setAllCollectors toggles the currently-wired collector set — reserved
// slots on EnrichmentOptions stay off until their collector lands.
func setAllCollectors(opts *EnrichmentOptions, on bool) {
	opts.HostSnapshot = on
	opts.BuildTimingSummary = on
	opts.BuildSettings = on
	opts.ActivityLog = on
	opts.ActivityLogAttach = on
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}

	return false
}
