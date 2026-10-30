package xcelerate

import "time"

// EnrichmentFlags carries the --enrich / --enrich-all / --enrich-none CLI
// flag values. Zero value = no CLI override; env/file win by default.
type EnrichmentFlags struct {
	Enabled []string
	All     bool
	None    bool
}

// LoadEnrichmentOptions returns a zero-value EnrichmentOptions until the
// full precedence loader lands (file < env < flags). Hook sites can safely
// call this today — AnyEnabled() stays false, so neither the wrapper nor
// the enricher pays any runtime cost.
func LoadEnrichmentOptions(_ *Config, _ func(string) string, _ EnrichmentFlags) EnrichmentOptions {
	return EnrichmentOptions{}
}

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
