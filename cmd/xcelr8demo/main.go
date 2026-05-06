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
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/invocations"
	"github.com/bitrise-io/go-utils/v2/log"
)

func main() {
	var (
		baseURL     = flag.String("base-url", "http://localhost:3000", "xcode-analytics-service base URL")
		orgSlug     = flag.String("org", "test-org", "Bitrise workspace / org slug")
		token       = flag.String("token", "", "Personal Access Token (optional in dev mode)")
		commitEmail = flag.String("commit-email", "", "filter invocations by commit email (ACI-4908)")
		hostname    = flag.String("hostname", "", "filter invocations by hostname / this-Mac-only (ACI-4909)")
		localOnly   = flag.Bool("local-only", false, "show only local invocations (provider id empty in DB; ACI-4910)")
		providerID  = flag.String("provider-id", "", "filter by CI provider id (e.g. bitrise / github); takes precedence over --local-only")
		limit       = flag.Int("limit", 10, "max rows to return")
	)
	flag.Parse()

	logger := log.NewLogger()
	client := invocations.NewDirectClient(*baseURL, *token, *orgSlug, logger)

	resp, err := client.List(invocations.DirectListFilter{
		CommitEmail: *commitEmail,
		Hostname:    *hostname,
		ProviderID:  *providerID,
		LocalOnly:   *localOnly,
		Limit:       *limit,
		// Wide window so seeded fixtures fall inside.
		After: time.Now().Add(-365 * 24 * time.Hour),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "list failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("totalCount=%d returned=%d filter.commitEmail=%q filter.hostname=%q filter.providerId=%q filter.localOnly=%v\n\n",
		resp.TotalCount, len(resp.Items), *commitEmail, *hostname, *providerID, *localOnly)

	for _, inv := range resp.Items {
		fmt.Printf("  %s  %s  %s  %s  provider=%q  hit=%.2f  %s\n",
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
