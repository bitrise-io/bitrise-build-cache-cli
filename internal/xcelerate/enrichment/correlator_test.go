//go:build unit

package enrichment_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestCorrelate_PicksBestOverlap(t *testing.T) {
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	entry := enrichment.ManifestEntry{
		Start: base.Add(2 * time.Second),
		Stop:  base.Add(8 * time.Second),
	}

	pending := []enrichment.PendingRecord{
		{InvocationID: "no-overlap-before", StartTime: base.Add(-5 * time.Minute), Duration: 10_000},
		{InvocationID: "small-overlap", StartTime: base.Add(7 * time.Second), Duration: 5_000},
		{InvocationID: "big-overlap", StartTime: base.Add(1 * time.Second), Duration: 8_000},
	}

	id, ok := enrichment.Correlate(entry, pending)
	assert.True(t, ok)
	assert.Equal(t, "big-overlap", id)
}

func TestCorrelate_NoMatch(t *testing.T) {
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	entry := enrichment.ManifestEntry{
		Start: base,
		Stop:  base.Add(5 * time.Second),
	}

	_, ok := enrichment.Correlate(entry, []enrichment.PendingRecord{
		{InvocationID: "far-future", StartTime: base.Add(time.Hour), Duration: 1000},
	})
	assert.False(t, ok)
}

func TestCorrelate_SlackWindow(t *testing.T) {
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	// entry starts 10s after pending window closes — within slack.
	entry := enrichment.ManifestEntry{
		Start: base.Add(15 * time.Second),
		Stop:  base.Add(25 * time.Second),
	}
	pending := []enrichment.PendingRecord{
		{InvocationID: "slack-match", StartTime: base, Duration: 5_000},
	}

	id, ok := enrichment.Correlate(entry, pending)
	assert.True(t, ok)
	assert.Equal(t, "slack-match", id)
}

func TestCorrelate_EmptyEntry(t *testing.T) {
	_, ok := enrichment.Correlate(enrichment.ManifestEntry{}, []enrichment.PendingRecord{
		{InvocationID: "x", StartTime: time.Now(), Duration: 1000},
	})
	assert.False(t, ok)
}
