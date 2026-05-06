package invocations

// ServiceProfile captures the per-tool differences between the four sink
// services so a single `DirectClient` can talk to any of them. Used by
// `NewDirectClientForTool` and `NewDirectClientWithProfile`.
//
// Productisation note: bazel (flare-bes) is intentionally not included
// here — its filter shape uses snake_case (`org_id`, `app_id`,
// `ci_provider`, `type=local|ci`) instead of the camelCase shape
// xcode/gradle/multiplatform share. A separate adapter would be needed.
type ServiceProfile struct {
	// Tool is the canonical build-tool identifier (matches `BuildTool…`
	// constants in this package).
	Tool string

	// DefaultBaseURL is the prod host. Override at construction for the
	// local `make dev-up` stack.
	DefaultBaseURL string

	// LocalBaseURL is the convention-over-config local-stack URL — handy
	// for the demo binary so a single flag picks the right port.
	LocalBaseURL string

	// InvocationsPath is where `GET /<list>` and `PUT /<list>/{id}` live.
	// Different per service: xcode/multiplatform use `/internal/invocations`
	// (internal endpoints), gradle uses `/builds` (history of naming
	// inside gradle-analytics-backend).
	InvocationsPath string

	// StatsPath is where `GET /<list>/stats` lives. Always
	// `<InvocationsPath>/stats` in practice, but kept explicit so a
	// future divergence doesn't surprise callers.
	StatsPath string
}

//nolint:gochecknoglobals
var (
	// XcodeProfile drives `xcode-analytics-service`.
	XcodeProfile = ServiceProfile{
		Tool:            BuildToolXcode,
		DefaultBaseURL:  "https://xcode-analytics.services.bitrise.io",
		LocalBaseURL:    "http://localhost:3000",
		InvocationsPath: "/internal/invocations",
		StatsPath:       "/internal/invocations/stats",
	}

	// MultiplatformProfile drives `multiplatform-analytics-service` (RN +
	// ccache).
	MultiplatformProfile = ServiceProfile{
		Tool:            BuildToolReactNative, // also handles BuildToolCcache; the service multiplexes.
		DefaultBaseURL:  "https://multiplatform-analytics.services.bitrise.io",
		LocalBaseURL:    "http://localhost:3001",
		InvocationsPath: "/internal/invocations",
		StatsPath:       "/internal/invocations/stats",
	}

	// GradleProfile drives `gradle-analytics-backend` (a.k.a. gradle-sink).
	GradleProfile = ServiceProfile{
		Tool:            BuildToolGradle,
		DefaultBaseURL:  "https://gradle-analytics.services.bitrise.io",
		LocalBaseURL:    "http://localhost:8081",
		InvocationsPath: "/builds",
		StatsPath:       "/builds/stats",
	}
)

// ProfileForTool maps a tool key to the matching `ServiceProfile`.
// Returns the zero `ServiceProfile{}` and `false` for tools the
// `DirectClient` cannot talk to today (e.g. bazel — different filter shape).
func ProfileForTool(tool string) (ServiceProfile, bool) {
	switch tool {
	case BuildToolXcode:
		return XcodeProfile, true
	case BuildToolGradle:
		return GradleProfile, true
	case BuildToolReactNative, BuildToolCcache:
		return MultiplatformProfile, true
	default:
		return ServiceProfile{}, false
	}
}
