// xcelr8demo is a hackathon-only smoke binary that exercises
// `internal/invocations.DirectClient` against a local xcode-analytics-service.
//
// Usage:
//
//	go run ./cmd/xcelr8demo --base-url http://localhost:3000 --org test-org --commit-email balazs@bitrise.io
//
// In dev mode the service treats the bearer token as the org slug, so passing
// --token is optional.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/invocations"
)

// toolKey normalises shorthand --service values so the demo accepts
// xcode|gradle|multiplatform|rn|ccache and feeds the right canonical
// BuildTool* constant into ProfileForTool.
func toolKey(s string) string {
	switch strings.ToLower(s) {
	case "xcode":
		return invocations.BuildToolXcode
	case "gradle":
		return invocations.BuildToolGradle
	case "multiplatform", "react-native", "rn", "ccache":
		return invocations.BuildToolReactNative
	default:
		return s
	}
}

func main() {
	var (
		service     = flag.String("service", "xcode", "Sink service: xcode | gradle | multiplatform (a.k.a. react-native / ccache)")
		baseURL     = flag.String("base-url", "", "Base URL override (defaults to the profile's local-stack URL)")
		orgSlug     = flag.String("org", "test-org", "Bitrise workspace / org slug")
		token       = flag.String("token", "", "Personal Access Token (optional in dev mode)")
		commitEmail = flag.String("commit-email", "", "filter invocations by commit email (ACI-4908)")
		hostname    = flag.String("hostname", "", "filter invocations by hostname / this-Mac-only (ACI-4909)")
		localOnly   = flag.Bool("local-only", false, "show only local invocations (provider id empty in DB; ACI-4910)")
		providerID  = flag.String("provider-id", "", "filter by CI provider id (e.g. bitrise / github); takes precedence over --local-only")
		stats       = flag.Bool("stats", false, "fetch aggregate stats (count, hit rate P50, time saved) instead of listing rows; ACI-4911")
		limit       = flag.Int("limit", 10, "max rows to return")
	)
	flag.Parse()

	profile, ok := invocations.ProfileForTool(toolKey(*service))
	if !ok {
		fmt.Fprintf(os.Stderr, "no DirectClient profile for --service=%q (try xcode / gradle / multiplatform)\n", *service)
		os.Exit(2)
	}

	resolvedBaseURL := *baseURL
	if resolvedBaseURL == "" {
		resolvedBaseURL = profile.LocalBaseURL
	}

	logger := log.NewLogger()
	client := invocations.NewDirectClientWithProfile(profile, resolvedBaseURL, *token, *orgSlug, logger)

	filter := invocations.DirectListFilter{
		CommitEmail: *commitEmail,
		Hostname:    *hostname,
		ProviderID:  *providerID,
		LocalOnly:   *localOnly,
		Limit:       *limit,
		// Wide window so seeded fixtures fall inside.
		After: time.Now().Add(-365 * 24 * time.Hour),
	}

	if *stats {
		s, err := client.Stats(filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stats failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stdout, "count=%d hitRateP50=%.3f timeSavedMs=%d (filter.commitEmail=%q hostname=%q providerId=%q localOnly=%v)\n",
			s.Count, s.HitRateP50, s.TimeSavedMs,
			*commitEmail, *hostname, *providerID, *localOnly)

		return
	}

	resp, err := client.List(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "totalCount=%d returned=%d filter.commitEmail=%q filter.hostname=%q filter.providerId=%q filter.localOnly=%v\n\n",
		resp.TotalCount, len(resp.Items), *commitEmail, *hostname, *providerID, *localOnly)

	for _, inv := range resp.Items {
		fmt.Fprintf(os.Stdout, "  %s  %s  %s  %s  provider=%q  hit=%.2f  %s\n",
			inv.InvocationDate.Format(time.RFC3339),
			inv.InvocationID,
			inv.Hostname,
			inv.CommitEmail,
			inv.ProviderID,
			inv.HitRate,
			inv.Branch,
		)
	}
}
