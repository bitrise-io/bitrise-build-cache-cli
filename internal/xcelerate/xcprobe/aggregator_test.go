//go:build unit

package xcprobe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/collect"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

func TestRun_zeroCollectors_returnsEmptyPayload(t *testing.T) {
	out := Run(context.Background(), xceleratconfig.EnrichmentOptions{}, collect.Input{}, nil)

	assert.Equal(t, payload.EnrichmentPayloadSchemaVersion, out.SchemaVersion)
	assert.Nil(t, out.Host)
	assert.Nil(t, out.BuildSettings)
	assert.Nil(t, out.BuildTimings)
	assert.Empty(t, out.CollectorErrors)
}
