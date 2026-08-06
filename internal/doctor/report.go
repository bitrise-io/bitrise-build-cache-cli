package doctor

// Check-name sets for Options.Only.
//
//nolint:gochecknoglobals
var (
	// XcodeCheckNames are the checks whose outcome can affect an xcodebuild
	// invocation. Deliberately excludes auth-backend: it costs a network
	// round-trip on every build. See AuthProbeCheckNames.
	XcodeCheckNames = []string{"auth", "xcelerate-proxy", "xcelerate-enrichment", "log-dirs"}

	// XcodeAnalyticsOnlyCheckNames drops the proxy, for a build with the cache off:
	// none is started, so reporting it as down is noise. Auth still gates the PUT.
	XcodeAnalyticsOnlyCheckNames = []string{"auth", "xcelerate-enrichment", "log-dirs"}

	// ReactNativeCheckNames are the checks whose outcome can affect a wrapped
	// React Native command. It drives Gradle, xcodebuild and ccache, so the
	// per-tool checks are all included — each skips itself when its tool isn't
	// activated. xcelerate-proxy is left out: the nested xcodebuild wrapper
	// starts it later, so it is legitimately down here.
	ReactNativeCheckNames = []string{
		"auth", "xcelerate-wrapper-path", "xcelerate-enrichment",
		"ccache-helper", "ccache-binary", "log-dirs",
	}

	// AuthProbeCheckNames verify the credential end-to-end, including the backend probe.
	AuthProbeCheckNames = []string{"auth", "auth-backend"}
)

// Opt-outs shared by every build-time health gate: the flag for one
// invocation, the env var for every one.
const (
	NoDoctorFlag  = "--no-doctor"
	EnvSkipDoctor = "BITRISE_BUILD_CACHE_SKIP_DOCTOR"
)

// EffectiveState is an item's state once an applied fix is taken into account.
func EffectiveState(it ReportItem) State {
	if it.FixResult != nil {
		return StateOK
	}

	return it.Result.State
}

func EffectiveOverall(r Report) State {
	worst := StateOK
	for _, it := range r.Items {
		switch EffectiveState(it) {
		case StateError:
			return StateError
		case StateWarn:
			worst = StateWarn
		case StateOK:
		}
	}

	return worst
}

func ItemDisplay(it ReportItem) (State, string) {
	if it.FixResult != nil {
		return StateOK, "fixed: " + *it.FixResult
	}

	return it.Result.State, it.Result.Detail
}

// Partition splits items into not-OK and OK, preserving check order.
func Partition(items []ReportItem) (issues, healthy []ReportItem) { //nolint:nonamedreturns // two same-typed returns benefit from labels
	for _, it := range items {
		if EffectiveState(it) == StateOK {
			healthy = append(healthy, it)
		} else {
			issues = append(issues, it)
		}
	}

	return issues, healthy
}

// Lines renders one plain "<state> <check>: <detail>" line per item.
func Lines(items []ReportItem) []string {
	if len(items) == 0 {
		return nil
	}

	lines := make([]string, 0, len(items))
	for _, it := range items {
		state, detail := ItemDisplay(it)
		lines = append(lines, string(state)+" "+it.Name+": "+detail)
	}

	return lines
}

// IssueLines is Lines restricted to the items that aren't OK.
func IssueLines(r Report) []string {
	issues, _ := Partition(r.Items)

	return Lines(issues)
}
