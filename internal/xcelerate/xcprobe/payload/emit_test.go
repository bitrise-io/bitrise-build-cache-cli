//go:build unit

package payload

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
)

func TestAttachToInvocation_populatesRawMessage(t *testing.T) {
	inv := &analytics.Invocation{}

	p := EnrichmentPayload{
		SchemaVersion: EnrichmentPayloadSchemaVersion,
		Host: &host.Snapshot{
			HostMetadata: configcommon.HostMetadata{OS: "Darwin"},
			Chip:         "Apple M4 Max",
			MemFreeMB:    16384,
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
		Host:          &host.Snapshot{HostMetadata: configcommon.HostMetadata{OS: "Darwin"}},
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

// TestAttachToInvocation_twoLargeSubRows_neverFailsOnSize documents the
// current contract from AttachToInvocation's docstring: at most one sub-row
// is dropped, and the attach step then succeeds even if the remainder is
// still over SoftCapBytes. The payload is "at-cap-or-over" after retry.
func TestAttachToInvocation_twoLargeSubRows_neverFailsOnSize(t *testing.T) {
	inv := &analytics.Invocation{}

	bigBuckets := map[string]BuildTimingBucket{}
	for i := 0; i < 1500; i++ {
		key := fmt.Sprintf("task-type-%s-%06d", strings.Repeat("x", 30), i)
		bigBuckets[key] = BuildTimingBucket{Count: i, TotalSeconds: float64(i)}
	}

	// Host is forced "large" via a long thermalPressure value so the
	// post-drop payload still exceeds SoftCapBytes after buildTimings is
	// evicted.
	bigHost := &host.Snapshot{
		HostMetadata:    configcommon.HostMetadata{OS: "Darwin"},
		ThermalPressure: strings.Repeat("y", SoftCapBytes+1024),
	}

	p := EnrichmentPayload{
		SchemaVersion: EnrichmentPayloadSchemaVersion,
		Host:          bigHost,
		BuildTimings:  &BuildTimingHistogram{Buckets: bigBuckets},
	}

	require.NoError(t, AttachToInvocation(inv, p), "attach must never fail solely on size")
	require.NotEmpty(t, inv.Enrichment, "payload must land on inv.Enrichment even when still over cap")

	var round EnrichmentPayload
	require.NoError(t, json.Unmarshal(inv.Enrichment, &round))

	// One sub-row (the larger) is dropped; the other survives even though it
	// alone exceeds the soft cap.
	droppedCount := 0
	if round.Host == nil {
		droppedCount++
	}
	if round.BuildTimings == nil {
		droppedCount++
	}
	assert.Equal(t, 1, droppedCount, "exactly one sub-row must be dropped per the single-pass contract")

	assert.Greater(t, len(inv.Enrichment), SoftCapBytes,
		"the surviving sub-row alone exceeds the soft cap; attach still succeeds")
	require.NotNil(t, round.CollectorErrors)
}
