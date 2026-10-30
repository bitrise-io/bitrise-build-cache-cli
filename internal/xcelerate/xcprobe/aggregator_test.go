//go:build unit

package xcprobe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/collect"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

type fakeCollector struct {
	name   string
	output collect.Output
	err    error
}

func (f fakeCollector) Name() string                                     { return f.name }
func (f fakeCollector) Collect(_ context.Context, _ collect.Input) (collect.Output, error) {
	return f.output, f.err
}

func TestRun_zeroCollectors_returnsEmptyPayload(t *testing.T) {
	out := Run(context.Background(), xceleratconfig.EnrichmentOptions{}, collect.Input{}, nil)

	assert.Equal(t, payload.EnrichmentPayloadSchemaVersion, out.SchemaVersion)
	assert.Nil(t, out.Host)
	assert.Nil(t, out.BuildSettings)
	assert.Nil(t, out.BuildTimings)
	assert.Empty(t, out.CollectorErrors)
}

func TestRun_buildTimingOnly_wiresJustThatCollector(t *testing.T) {
	tail := []byte("CompileSwift (10 tasks) | 2.5 seconds\n")

	out := Run(
		context.Background(),
		xceleratconfig.EnrichmentOptions{BuildTimingSummary: true},
		collect.Input{XcodebuildInfoBytes: tail},
		nil,
	)

	require.NotNil(t, out.BuildTimings)
	assert.Equal(t, 10, out.BuildTimings.Buckets["CompileSwift"].Count)
	assert.Nil(t, out.Host)
	assert.Nil(t, out.BuildSettings)
	assert.Empty(t, out.CollectorErrors)
}

// TestRunWith_oneCollectorErrors_othersStillPopulate asserts the aggregator's
// isolation contract: one collector's error lands under CollectorErrors keyed
// by its Name(), while sibling collectors' outputs still populate.
func TestRunWith_oneCollectorErrors_othersStillPopulate(t *testing.T) {
	broken := fakeCollector{name: "broken", err: errors.New("boom")}

	okSnap := &host.Snapshot{HostMetadata: configcommon.HostMetadata{OS: "Darwin"}}
	healthy := fakeCollector{name: "healthy", output: collect.Output{Host: okSnap}}

	settingsSnap := &payload.BuildSettingsSnapshot{SwiftOptimizationLevel: "-O"}
	settings := fakeCollector{name: "settings", output: collect.Output{BuildSettings: settingsSnap}}

	out := runWith(context.Background(), 2*time.Second, []collect.Collector{broken, healthy, settings}, collect.Input{}, nil)

	require.NotNil(t, out.CollectorErrors)
	assert.Equal(t, "boom", out.CollectorErrors["broken"])
	assert.NotContains(t, out.CollectorErrors, "healthy")
	assert.NotContains(t, out.CollectorErrors, "settings")

	require.NotNil(t, out.Host)
	assert.Equal(t, "Darwin", out.Host.OS)
	require.NotNil(t, out.BuildSettings)
	assert.Equal(t, "-O", out.BuildSettings.SwiftOptimizationLevel)
}
