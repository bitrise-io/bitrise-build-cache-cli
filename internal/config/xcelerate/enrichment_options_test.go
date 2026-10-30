//go:build unit

package xcelerate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEnrichmentOptions_AnyEnabled_zeroValue(t *testing.T) {
	assert.False(t, EnrichmentOptions{}.AnyEnabled())
}

func TestEnrichmentOptions_AnyEnabled_eachPhase0Toggle(t *testing.T) {
	cases := []struct {
		name string
		opts EnrichmentOptions
	}{
		{name: "hostSnapshot", opts: EnrichmentOptions{HostSnapshot: true}},
		{name: "buildTimingSummary", opts: EnrichmentOptions{BuildTimingSummary: true}},
		{name: "buildSettings", opts: EnrichmentOptions{BuildSettings: true}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.True(t, c.opts.AnyEnabled())
		})
	}
}

func TestEnrichmentOptions_AnyEnabled_ignoresNonBoolFields(t *testing.T) {
	opts := EnrichmentOptions{
		SwiftTopN:           100,
		ClangTopN:           50,
		PerCollectorTimeout: 30 * time.Second,
		FailOpen:            true,
	}

	assert.False(t, opts.AnyEnabled(), "non-collector-toggle fields must not make AnyEnabled true")
}
