package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

const (
	enrichmentMinConsecutiveErrorsToWarn = 3
	// enrichmentStaleLastSuccessAge aliases Retrier.MaxAge — after that long
	// without a successful PUT and with pending records still queued, we surface
	// a warning. Kept coupled so it never drifts from the retry give-up window.
	enrichmentStaleLastSuccessAge  = enrichment.DefaultRetryMaxAge
	enrichmentDashboardURLTemplate = "https://app.bitrise.io/build-cache/invocations/xcode/%s"
	enrichmentPendingDetailMax     = 3
	enrichmentLastErrorSnippetMax  = 120
)

func (d *Doctor) enrichmentCheck() Check {
	return Check{
		Name: "xcelerate-enrichment",
		Diagnose: func(_ context.Context) Result {
			if !d.toolActivated(toolconfig.Xcelerate) {
				return Result{State: StateOK, Detail: "skipped (xcode not activated)"}
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return Result{State: StateError, Detail: "resolve home dir: " + err.Error()}
			}

			p := paths.FromHome(home)

			return diagnoseEnrichment(p.EnrichmentHealthFile(), p.PendingInvocationsFile(), d.now)
		},
	}
}

func (d *Doctor) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}

	return time.Now()
}

func diagnoseEnrichment(healthPath, pendingPath string, now func() time.Time) Result {
	pendingStore := &enrichment.Store{Path: pendingPath}

	pending, err := pendingStore.Load()
	if err != nil {
		return Result{State: StateError, Detail: "load pending: " + err.Error()}
	}

	oldestPending := oldestPendingAge(pending, now())

	snap, err := enrichment.LoadHealth(healthPath)
	if errors.Is(err, os.ErrNotExist) {
		return Result{State: StateOK, Detail: "no enrichment attempts yet"}
	}

	if err != nil {
		return Result{State: StateError, Detail: "load enrichment health: " + err.Error()}
	}

	switch {
	case snap.ConsecutiveErrors >= enrichmentMinConsecutiveErrorsToWarn:
		detail := fmt.Sprintf("%d consecutive failures — last: %s", snap.ConsecutiveErrors, snap.LastError)
		if !snap.LastErrorAt.IsZero() {
			detail += "\n  lastErrorAt=" + snap.LastErrorAt.Format(time.RFC3339)
		}

		return Result{State: StateWarn, Detail: detail}
	case oldestPending > enrichment.DefaultRetryMaxAge:
		detail := fmt.Sprintf("pending invocation queue stale (oldest %s > %s)", oldestPending.Round(time.Second), enrichment.DefaultRetryMaxAge)
		if extra := formatPendingDetail(pending); extra != "" {
			detail += "\n" + extra
		}

		return Result{State: StateWarn, Detail: detail}
	case !snap.LastSuccess.IsZero() && now().Sub(snap.LastSuccess) > enrichmentStaleLastSuccessAge && len(pending) > 0:
		detail := fmt.Sprintf("no successful enrichment in %s and %d pending invocations", now().Sub(snap.LastSuccess).Round(time.Minute), len(pending))
		if extra := formatPendingDetail(pending); extra != "" {
			detail += "\n" + extra
		}

		return Result{State: StateWarn, Detail: detail}
	}

	return Result{State: StateOK, Detail: fmt.Sprintf("healthy (last success %s, %d pending)", formatOptionalTime(snap.LastSuccess), len(pending))}
}

// formatOptionalTime renders t as RFC3339 or "never" when zero — avoids the
// misleading "0001-01-01T00:00:00Z" leak when a status snapshot has no
// successful attempt yet.
func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	return t.Format(time.RFC3339)
}

func oldestPendingAge(records []enrichment.PendingRecord, now time.Time) time.Duration {
	var oldest time.Duration
	for _, r := range records {
		if r.StartTime.IsZero() {
			continue
		}

		age := now.Sub(r.StartTime)
		if age > oldest {
			oldest = age
		}
	}

	return oldest
}

func oldestPendingRecords(records []enrichment.PendingRecord, n int) []enrichment.PendingRecord {
	if n <= 0 || len(records) == 0 {
		return nil
	}

	sorted := make([]enrichment.PendingRecord, len(records))
	copy(sorted, records)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].StartTime.Before(sorted[j].StartTime)
	})

	if len(sorted) > n {
		sorted = sorted[:n]
	}

	return sorted
}

func formatPendingDetail(records []enrichment.PendingRecord) string {
	top := oldestPendingRecords(records, enrichmentPendingDetailMax)
	if len(top) == 0 {
		return ""
	}

	var b strings.Builder
	for i, r := range top {
		if i > 0 {
			b.WriteByte('\n')
		}

		fmt.Fprintf(&b, "  - %s startedAt=%s attempts=%d",
			r.InvocationID, formatOptionalTime(r.StartTime), r.Attempts)

		if r.LastError != "" {
			fmt.Fprintf(&b, " lastError=%s", truncateSingleLine(r.LastError, enrichmentLastErrorSnippetMax))
		}

		fmt.Fprintf(&b, " "+enrichmentDashboardURLTemplate, r.InvocationID)
	}

	return b.String()
}

func truncateSingleLine(s string, limit int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")

	if len(s) > limit {
		return s[:limit]
	}

	return s
}
