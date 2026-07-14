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

	// Older-than-EnrichmentPendingMaxAge pending record.
	store := &enrichment.Store{Path: pendingPath, Now: func() time.Time { return now.Add(-2 * enrichment.EnrichmentPendingMaxAge) }}
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "stale",
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
