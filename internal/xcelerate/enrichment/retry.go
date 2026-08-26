package enrichment

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

const (
	DefaultRetryInterval = 5 * time.Minute
	DefaultRetryMaxAge   = 24 * time.Hour
)

type Retrier struct {
	Store    *Store
	Client   InvocationPutter
	Interval time.Duration
	MaxAge   time.Duration
	Now      func() time.Time
	Logger   log.Logger
}

func (r *Retrier) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}

	return time.Now()
}

func (r *Retrier) Run(ctx context.Context) {
	interval := r.Interval
	if interval == 0 {
		interval = DefaultRetryInterval
	}

	if r.MaxAge == 0 {
		r.MaxAge = DefaultRetryMaxAge
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Sweep once before waiting out the first tick: a restart is exactly when
	// orphans from the previous process are sitting in the store, and holding
	// them for a full interval leaves them visible to the doctor's pending check.
	r.Sweep()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Sweep()
		}
	}
}

// Sweep loads a snapshot, runs PutInvocation for each retried record outside
// the store lock, then merges the outcomes back under Mutate so records
// appended concurrently by Enricher/slim-emit survive.
func (r *Retrier) Sweep() {
	if r.Store == nil {
		return
	}

	logger := logOr(r.Logger)

	snapshot, err := r.Store.Load()
	if err != nil {
		logger.Warnf("Retrier failed to load pending: %s", err)

		return
	}

	logger.Infof("Retrier: sweep start, pending=%d", len(snapshot))

	now := r.now()
	maxAge := r.MaxAge
	if maxAge == 0 {
		maxAge = DefaultRetryMaxAge
	}

	// Store increments as deltas rather than snapshot-time totals so an
	// Enricher-side bump landing between snapshot and merge is preserved.
	type update struct {
		drop          bool
		attemptsDelta int
		lastAt        time.Time
		lastErr       string
	}
	updates := map[string]update{}

	for _, rec := range snapshot {
		if rec.Attempts == 0 {
			continue
		}

		if !rec.FirstAttempt.IsZero() && now.Sub(rec.FirstAttempt) > maxAge {
			logger.Warnf("Enrichment retry gave up on %s after %s (attempts=%d)", rec.InvocationID, now.Sub(rec.FirstAttempt).Round(time.Second), rec.Attempts)
			updates[rec.InvocationID] = update{drop: true}

			continue
		}

		var inv analytics.Invocation
		if err := json.Unmarshal(rec.EnrichedPayload, &inv); err != nil {
			logger.Warnf("Retrier failed to decode payload for %s: %s — dropping", rec.InvocationID, err)
			updates[rec.InvocationID] = update{drop: true}

			continue
		}

		if putErr := r.Client.PutInvocation(inv); putErr != nil {
			updates[rec.InvocationID] = update{attemptsDelta: 1, lastAt: now, lastErr: putErr.Error()}
			logger.Warnf("Retrier PUT failed for %s (attempts=%d): %s", rec.InvocationID, rec.Attempts+1, putErr)

			continue
		}

		logger.Infof("Retrier PUT succeeded for %s (attempts=%d)", rec.InvocationID, rec.Attempts+1)
		updates[rec.InvocationID] = update{drop: true}
	}

	if len(updates) > 0 {
		if err := r.Store.Mutate(func(current []PendingRecord) []PendingRecord {
			out := current[:0]
			for _, rec := range current {
				u, seen := updates[rec.InvocationID]
				if !seen {
					out = append(out, rec)

					continue
				}
				if u.drop {
					continue
				}

				rec.Attempts += u.attemptsDelta
				rec.LastAttempt = u.lastAt
				rec.LastError = u.lastErr
				out = append(out, rec)
			}

			return out
		}); err != nil {
			logger.Warnf("Retrier failed to persist swept pending: %s", err)
		}
	}

	r.pruneStrandedOrphans(now, maxAge, logger)
}

// pruneStrandedOrphans reuses the exact predicate PruneAll trusts to drain
// records that the watcher-side path never touches: slim-emitted entries
// (Attempts==0) whose enrichment path never landed.
func (r *Retrier) pruneStrandedOrphans(now time.Time, maxAge time.Duration, logger log.Logger) {
	before, err := r.Store.Load()
	if err != nil {
		logger.Debugf("Retrier: prune load failed: %s", err)

		return
	}

	if err := r.Store.PruneOrphansOlderThan(now, maxAge); err != nil {
		logger.Warnf("Retrier: prune orphans failed: %s", err)

		return
	}

	after, err := r.Store.Load()
	if err != nil {
		logger.Debugf("Retrier: prune reload failed: %s", err)

		return
	}

	if pruned := len(before) - len(after); pruned > 0 {
		logger.Infof("Retrier: pruned %d stranded orphan record(s)", pruned)
	}
}
