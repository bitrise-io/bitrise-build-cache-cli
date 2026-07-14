package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

const (
	enrichmentMinConsecutiveErrorsToWarn = 3
	enrichmentStaleLastSuccessAge        = 24 * time.Hour
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
		return Result{
			State:  StateWarn,
			Detail: fmt.Sprintf("%d consecutive failures — last: %s", snap.ConsecutiveErrors, snap.LastError),
		}
	case oldestPending > enrichment.EnrichmentPendingMaxAge:
		return Result{
			State:  StateWarn,
			Detail: fmt.Sprintf("pending invocation queue stale (oldest %s > %s)", oldestPending.Round(time.Second), enrichment.EnrichmentPendingMaxAge),
		}
	case !snap.LastSuccess.IsZero() && now().Sub(snap.LastSuccess) > enrichmentStaleLastSuccessAge && len(pending) > 0:
		return Result{
			State:  StateWarn,
			Detail: fmt.Sprintf("no successful enrichment in %s and %d pending invocations", now().Sub(snap.LastSuccess).Round(time.Minute), len(pending)),
		}
	}

	return Result{State: StateOK, Detail: fmt.Sprintf("healthy (last success %s, %d pending)", snap.LastSuccess.Format(time.RFC3339), len(pending))}
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
