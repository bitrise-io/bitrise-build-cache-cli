//go:build unit

package xcode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/collect"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// TestNewEnrichmentHook_hookSiteSymmetry exercises the IDE hook's gating.
// Mirrors TestAttachEnrichment_hookSiteSymmetry on the wrapper side so both
// hook sites keep the same short-circuit contract.
func TestNewEnrichmentHook_hookSiteSymmetry(t *testing.T) {
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

			restore := swapEnrichmentRunFn(t, func(_ context.Context, _ xceleratconfig.EnrichmentOptions, _ collect.Input, _ log.Logger, inv *analytics.Invocation) {
				runCalls++

				out := payload.EnrichmentPayload{
					SchemaVersion: payload.EnrichmentPayloadSchemaVersion,
					Host:          &host.Snapshot{HostMetadata: configcommon.HostMetadata{OS: "Darwin"}},
				}

				raw, err := json.Marshal(out) //nolint:musttag // see payload/emit.go
				require.NoError(t, err)
				inv.Enrichment = raw
			})
			t.Cleanup(restore)

			hook := newEnrichmentHook(bundleTestLogger)

			inv := &analytics.Invocation{InvocationID: "test-inv"}
			group := enrichment.ManifestEntryGroup{
				Entries: []enrichment.ManifestEntry{{
					UUID:       "uuid",
					SchemeName: "Scheme",
					Signature:  "Build Scheme",
				}},
			}

			hook(context.Background(), "/tmp/dd/Logs/Build/LogStoreManifest.plist", group, configcommon.HostMetadata{OS: "Darwin"}, "", inv)

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

func swapEnrichmentRunFn(_ *testing.T, fake func(context.Context, xceleratconfig.EnrichmentOptions, collect.Input, log.Logger, *analytics.Invocation)) func() {
	original := enrichmentRunFn
	enrichmentRunFn = fake

	return func() { enrichmentRunFn = original }
}
