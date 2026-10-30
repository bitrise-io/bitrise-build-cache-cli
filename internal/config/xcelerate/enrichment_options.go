package xcelerate

import (
	"strings"
	"time"
)

// EnrichmentOptions declares which xcprobe collectors run and under what
// bounds. Zero value = all collectors off. CLI > env > file precedence is
// applied by LoadEnrichmentOptions.
type EnrichmentOptions struct {
	// Phase 0 — always-opt-in, non-perturbing.
	HostSnapshot       bool
	BuildTimingSummary bool
	BuildSettings      bool

	// Phase 1+ reserved slots. Keep them in the struct so the loader /
	// precedence shape is stable across phases; the aggregator still only
	// instantiates what Phase 0 ships.
	ActivityLog       bool
	ActivityLogAttach bool

	SwiftFunctionTimes bool
	SwiftTypeChecks    bool
	SwiftStats         bool
	ClangTimeTrace     bool
	LinkerStats        bool

	BuildDB bool

	SwiftTopN int
	ClangTopN int

	PerCollectorTimeout time.Duration
	FailOpen            bool
}

// AnyEnabled reports whether any collector bool is set. Both the wrapper and
// the enricher hook sites check this and short-circuit when false, so a
// disabled-by-default EnrichmentOptions pays zero runtime cost.
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

// EnrichmentFlags carries the --enrich / --enrich-all / --enrich-none CLI
// flag values. Zero value = no CLI override; env/file win.
type EnrichmentFlags struct {
	Enabled []string
	All     bool
	None    bool
}

const (
	// EnvEnrichmentCollectors enables individual collectors via a
	// comma-separated list, e.g.
	// BITRISE_BUILD_CACHE_ENRICH=hostsnap,buildtiming,buildsettings
	EnvEnrichmentCollectors = "BITRISE_BUILD_CACHE_ENRICH"
	// EnvEnrichmentAll turns every collector on (short-circuits individual).
	EnvEnrichmentAll = "BITRISE_BUILD_CACHE_ENRICH_ALL"
	// EnvEnrichmentNone forces every collector off, overriding any other setting.
	EnvEnrichmentNone = "BITRISE_BUILD_CACHE_ENRICH_NONE"

	// Collector tokens accepted on the env list / --enrich flag. Mirror the
	// field names on EnrichmentOptions (lower-kebab) so the user wire format
	// stays stable across phases.
	collectorHostSnap       = "hostsnap"
	collectorBuildTiming    = "buildtiming"
	collectorBuildSettings  = "buildsettings"
	collectorAll            = "all"
	collectorNone           = "none"
)

// LoadEnrichmentOptions resolves EnrichmentOptions from the three sources,
// lowest precedence first: xcelerate config file (not read in Phase 0 —
// reserved for a later phase that persists collector preferences), env, CLI
// flags. CLI --enrich-none wins over everything; env ENRICH_NONE wins over
// env ENRICH_ALL / ENRICH; a bare --enrich-all turns everything on.
//
// env=nil is tolerated (treated as a function returning ""); the Phase 0
// hook sites pass os.Getenv.
func LoadEnrichmentOptions(_ *Config, env func(string) string, flags EnrichmentFlags) EnrichmentOptions {
	if env == nil {
		env = func(string) string { return "" }
	}

	opts := EnrichmentOptions{
		PerCollectorTimeout: 10 * time.Second,
		FailOpen:            true,
	}

	// env layer.
	applyCollectorList(&opts, parseCollectorList(env(EnvEnrichmentCollectors)))

	if truthy(env(EnvEnrichmentAll)) {
		setAllPhase0(&opts, true)
	}

	if truthy(env(EnvEnrichmentNone)) {
		setAllPhase0(&opts, false)
	}

	// flag layer — CLI wins over env.
	applyCollectorList(&opts, flags.Enabled)

	if flags.All {
		setAllPhase0(&opts, true)
	}

	if flags.None {
		setAllPhase0(&opts, false)
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
		case collectorAll:
			setAllPhase0(opts, true)
		case collectorNone:
			setAllPhase0(opts, false)
		}
	}
}

func setAllPhase0(opts *EnrichmentOptions, on bool) {
	opts.HostSnapshot = on
	opts.BuildTimingSummary = on
	opts.BuildSettings = on
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}

	return false
}
