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

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Sweep()
		}
	}
}

func (r *Retrier) Sweep() {
	if r.Store == nil {
		return
	}

	records, err := r.Store.Load()
	if err != nil {
		if r.Logger != nil {
			r.Logger.Warnf("Retrier failed to load pending: %s", err)
		}

		return
	}

	now := r.now()
	kept := records[:0]

	for _, rec := range records {
		if rec.Attempts == 0 {
			kept = append(kept, rec)

			continue
		}

		if !rec.FirstAttempt.IsZero() && now.Sub(rec.FirstAttempt) > r.MaxAge {
			if r.Logger != nil {
				r.Logger.Warnf("Enrichment retry gave up on %s after %s (attempts=%d)", rec.InvocationID, now.Sub(rec.FirstAttempt).Round(time.Second), rec.Attempts)
			}

			continue
		}

		var inv analytics.Invocation
		if err := json.Unmarshal(rec.EnrichedPayload, &inv); err != nil {
			if r.Logger != nil {
				r.Logger.Warnf("Retrier failed to decode payload for %s: %s — dropping", rec.InvocationID, err)
			}

			continue
		}

		if putErr := r.Client.PutInvocation(inv); putErr != nil {
			rec.Attempts++
			rec.LastAttempt = now
			rec.LastError = putErr.Error()
			kept = append(kept, rec)

			if r.Logger != nil {
				r.Logger.Warnf("Retrier PUT failed for %s (attempts=%d): %s", rec.InvocationID, rec.Attempts, putErr)
			}

			continue
		}

		if r.Logger != nil {
			r.Logger.Debugf("Retrier PUT succeeded for %s (attempts=%d)", rec.InvocationID, rec.Attempts+1)
		}
	}

	if err := r.Store.Save(kept); err != nil && r.Logger != nil {
		r.Logger.Warnf("Retrier failed to persist swept pending: %s", err)
	}
}
