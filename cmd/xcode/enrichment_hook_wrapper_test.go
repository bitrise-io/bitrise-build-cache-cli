//go:build unit

package xcode

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/collect"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// TestAttachEnrichment_hookSiteSymmetry exercises the wrapper's
// attachEnrichment gating. Mirrors TestNewEnrichmentHook_hookSiteSymmetry on
// the IDE side so both hook sites keep the same short-circuit contract.
func TestAttachEnrichment_hookSiteSymmetry(t *testing.T) {
	for _, tc := range []struct {
		name       string
		envValue   string
		wantAttach bool
	}{
		{name: "disabled_no_env", envValue: "", wantAttach: false},
		{name: "enabled_hostsnap", envValue: "hostsnap", wantAttach: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(xceleratconfig.EnvEnrichmentCollectors, tc.envValue)

			var runCalls int

			restore := swapXcprobeRunFn(t, func(_ context.Context, _ xceleratconfig.EnrichmentOptions, _ collect.Input, _ log.Logger) payload.EnrichmentPayload {
				runCalls++

				return payload.EnrichmentPayload{
					SchemaVersion: payload.EnrichmentPayloadSchemaVersion,
					Host:          &host.Snapshot{HostMetadata: configcommon.HostMetadata{OS: "Darwin"}},
				}
			})
			t.Cleanup(restore)

			argsMock := &xcodeargsMocks.XcodeArgsMock{
				DerivedDataPathFunc: func() string { return "/tmp/dd" },
				ProjectDirFunc:      func() string { return "/tmp/proj" },
			}

			runner := &XcodebuildRunner{
				Logger:    bundleTestLogger,
				XcodeArgs: argsMock,
				Metadata:  configcommon.CacheConfigMetadata{HostMetadata: configcommon.HostMetadata{OS: "Darwin"}},
			}

			inv := &analytics.Invocation{InvocationID: "test-inv"}
			runStats := xcodeargs.RunStats{StartTime: time.Now(), DurationMS: 100}

			runner.attachEnrichment(context.Background(), inv, runStats)

			if tc.wantAttach {
				require.Equal(t, 1, runCalls, "aggregator must run when AnyEnabled() is true")
				require.NotEmpty(t, inv.Enrichment, "attached payload must land on inv.Enrichment")

				var decoded payload.EnrichmentPayload
				require.NoError(t, json.Unmarshal(inv.Enrichment, &decoded))
				assert.Equal(t, payload.EnrichmentPayloadSchemaVersion, decoded.SchemaVersion)
			} else {
				assert.Zero(t, runCalls, "aggregator must not run when AnyEnabled() is false")
				assert.Empty(t, inv.Enrichment, "inv.Enrichment must stay empty when no collector is on")
			}
		})
	}
}

func swapXcprobeRunFn(_ *testing.T, fake func(context.Context, xceleratconfig.EnrichmentOptions, collect.Input, log.Logger) payload.EnrichmentPayload) func() {
	original := xcprobeRunFn
	xcprobeRunFn = fake

	return func() { xcprobeRunFn = original }
}
