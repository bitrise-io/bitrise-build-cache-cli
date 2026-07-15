//go:build unit

package doctor

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestEnrichmentCheck_skippedWhenNotActivated(t *testing.T) {
	d := &Doctor{ActivatedTools: func() map[toolconfig.Tool]bool { return nil }}

	res := d.enrichmentCheck().Diagnose(nil)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "skipped")
}

func TestEnrichmentCheck_okWhenNoHealthFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	d := &Doctor{ActivatedTools: func() map[toolconfig.Tool]bool { return map[toolconfig.Tool]bool{toolconfig.Xcelerate: true} }}

	res := d.enrichmentCheck().Diagnose(nil)
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "no enrichment attempts yet")
}

func TestEnrichmentCheck_warnOnConsecutiveErrors(t *testing.T) {
	tmp := t.TempDir()
	healthPath := filepath.Join(tmp, "health.json")
	pendingPath := filepath.Join(tmp, "pending.ndjson")
	now := func() time.Time { return time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC) }

	hw := &enrichment.HealthWriter{Path: healthPath}
	require.NoError(t, hw.Update(func(s *enrichment.HealthSnapshot) {
		s.ConsecutiveErrors = 3
		s.LastError = "boom"
	}))

	res := diagnoseEnrichment(healthPath, pendingPath, now)
	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "3 consecutive failures")
	assert.Contains(t, res.Detail, "boom")
}

func TestEnrichmentCheck_warnOnStalePending(t *testing.T) {
	tmp := t.TempDir()
	healthPath := filepath.Join(tmp, "health.json")
	pendingPath := filepath.Join(tmp, "pending.ndjson")
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	// Four pending records with staggered StartTimes; all older than
	// EnrichmentPendingMaxAge so writeAtomic keeps them (Append prunes on time.Now
	// relative to Store.Now, not the record's own StartTime).
	store := &enrichment.Store{Path: pendingPath, Now: func() time.Time { return now.Add(-10 * enrichment.EnrichmentPendingMaxAge) }}
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "oldest",
		StartTime:    now.Add(-5 * enrichment.EnrichmentPendingMaxAge),
	}))
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "older",
		StartTime:    now.Add(-4 * enrichment.EnrichmentPendingMaxAge),
	}))
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "old",
		StartTime:    now.Add(-3 * enrichment.EnrichmentPendingMaxAge),
	}))
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "newest",
		StartTime:    now.Add(-2 * enrichment.EnrichmentPendingMaxAge),
	}))

	// Recent successful health so we don't hit the "no successful enrichment" branch.
	hw := &enrichment.HealthWriter{Path: healthPath}
	require.NoError(t, hw.Update(func(s *enrichment.HealthSnapshot) {
		s.LastSuccess = now.Add(-time.Minute)
		s.LastAttempt = now.Add(-time.Minute)
	}))

	res := diagnoseEnrichment(healthPath, pendingPath, func() time.Time { return now })
	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "pending invocation queue stale")
	assert.Contains(t, res.Detail, "oldest")
	assert.Contains(t, res.Detail, "older")
	assert.Contains(t, res.Detail, "old ")
	assert.NotContains(t, res.Detail, "newest", "only oldest 3 should be enumerated")
	assert.Contains(t, res.Detail, "https://app.bitrise.io/build-cache/invocations/xcode/")
}

func TestEnrichmentCheck_warnOnStaleLastSuccess(t *testing.T) {
	tmp := t.TempDir()
	healthPath := filepath.Join(tmp, "health.json")
	pendingPath := filepath.Join(tmp, "pending.ndjson")
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	// A pending entry within the freshness window.
	store := &enrichment.Store{Path: pendingPath, Now: func() time.Time { return now }}
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "fresh",
		StartTime:    now.Add(-time.Minute),
	}))

	hw := &enrichment.HealthWriter{Path: healthPath}
	require.NoError(t, hw.Update(func(s *enrichment.HealthSnapshot) {
		s.LastSuccess = now.Add(-48 * time.Hour)
		s.LastAttempt = now.Add(-time.Minute)
	}))

	res := diagnoseEnrichment(healthPath, pendingPath, func() time.Time { return now })
	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "no successful enrichment")
	assert.Contains(t, res.Detail, "https://app.bitrise.io/build-cache/invocations/xcode/fresh")
}

func TestEnrichmentCheck_okWhenHealthy(t *testing.T) {
	tmp := t.TempDir()
	healthPath := filepath.Join(tmp, "health.json")
	pendingPath := filepath.Join(tmp, "pending.ndjson")
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	hw := &enrichment.HealthWriter{Path: healthPath}
	require.NoError(t, hw.Update(func(s *enrichment.HealthSnapshot) {
		s.LastSuccess = now.Add(-time.Minute)
		s.LastAttempt = now.Add(-time.Minute)
	}))

	res := diagnoseEnrichment(healthPath, pendingPath, func() time.Time { return now })
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "healthy")
}


func TestEnrichmentCheck_warnOnConsecutiveErrors_includesLastErrorAt(t *testing.T) {
	tmp := t.TempDir()
	healthPath := filepath.Join(tmp, "health.json")
	pendingPath := filepath.Join(tmp, "pending.ndjson")
	now := func() time.Time { return time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC) }
	errAt := time.Date(2026, 7, 14, 9, 55, 0, 0, time.UTC)

	hw := &enrichment.HealthWriter{Path: healthPath}
	require.NoError(t, hw.Update(func(s *enrichment.HealthSnapshot) {
		s.ConsecutiveErrors = 5
		s.LastError = "backend 503"
		s.LastErrorAt = errAt
	}))

	res := diagnoseEnrichment(healthPath, pendingPath, now)
	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "5 consecutive failures")
	assert.Contains(t, res.Detail, "backend 503")
	assert.Contains(t, res.Detail, "lastErrorAt="+errAt.Format(time.RFC3339))
}
