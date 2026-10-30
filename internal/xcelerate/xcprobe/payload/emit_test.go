//go:build unit

package payload

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

func TestAttachToInvocation_populatesRawMessage(t *testing.T) {
	inv := &analytics.Invocation{}

	p := EnrichmentPayload{
		SchemaVersion: EnrichmentPayloadSchemaVersion,
		Host: &HostSnapshot{
			OS:    "Darwin",
			Chip:  "Apple M4 Max",
			MemFreeMB: 16384,
		},
	}

	require.NoError(t, AttachToInvocation(inv, p))

	require.NotEmpty(t, inv.Enrichment)

	var round EnrichmentPayload
	require.NoError(t, json.Unmarshal(inv.Enrichment, &round))
	assert.Equal(t, EnrichmentPayloadSchemaVersion, round.SchemaVersion)
	require.NotNil(t, round.Host)
	assert.Equal(t, "Darwin", round.Host.OS)
	assert.Equal(t, "Apple M4 Max", round.Host.Chip)
	assert.Equal(t, uint64(16384), round.Host.MemFreeMB)
}

func TestAttachToInvocation_dropsLargestSubRow_whenOverSoftCap(t *testing.T) {
	inv := &analytics.Invocation{}

	// Craft a buildTimings histogram large enough to cross SoftCapBytes;
	// keep host + buildSettings small so buildTimings is unambiguously the
	// biggest row.
	bigBuckets := map[string]BuildTimingBucket{}
	// Each key is ~50 bytes, each row serialises to ~80 bytes; aim well
	// above 64 KB.
	for i := 0; i < 1500; i++ {
		key := fmt.Sprintf("task-type-%s-%06d", strings.Repeat("x", 30), i)
		bigBuckets[key] = BuildTimingBucket{
			Count:        i,
			TotalSeconds: float64(i),
		}
	}

	p := EnrichmentPayload{
		SchemaVersion: EnrichmentPayloadSchemaVersion,
		Host:          &HostSnapshot{OS: "Darwin"},
		BuildSettings: &BuildSettingsSnapshot{SwiftOptimizationLevel: "-Onone"},
		BuildTimings:  &BuildTimingHistogram{Buckets: bigBuckets},
	}

	require.NoError(t, AttachToInvocation(inv, p))

	var round EnrichmentPayload
	require.NoError(t, json.Unmarshal(inv.Enrichment, &round))

	assert.Nil(t, round.BuildTimings, "largest sub-row (buildTimings) must be dropped")
	require.NotNil(t, round.Host, "smaller sub-rows must survive the drop")
	require.NotNil(t, round.BuildSettings)
	assert.Equal(t, "Darwin", round.Host.OS)
	assert.Equal(t, "-Onone", round.BuildSettings.SwiftOptimizationLevel)

	require.NotNil(t, round.CollectorErrors)
	assert.Contains(t, round.CollectorErrors, "buildTimings")
	assert.Contains(t, round.CollectorErrors["buildTimings"], "dropped")
	assert.LessOrEqual(t, len(inv.Enrichment), SoftCapBytes*2,
		"attach must stay well under the cap after dropping the largest sub-row")
}

func TestAttachToInvocation_nilInvocation_errors(t *testing.T) {
	err := AttachToInvocation(nil, EnrichmentPayload{SchemaVersion: EnrichmentPayloadSchemaVersion})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil invocation")
}
