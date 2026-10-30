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

func TestLoadEnrichmentOptions_defaults_allOff(t *testing.T) {
	opts := LoadEnrichmentOptions(nil, nil, EnrichmentFlags{})
	assert.False(t, opts.AnyEnabled())
	assert.Equal(t, 10*time.Second, opts.PerCollectorTimeout)
	assert.True(t, opts.FailOpen)
}

func TestLoadEnrichmentOptions_envList_enablesIndividual(t *testing.T) {
	env := func(k string) string {
		if k == EnvEnrichmentCollectors {
			return "hostsnap, buildtiming"
		}

		return ""
	}

	opts := LoadEnrichmentOptions(nil, env, EnrichmentFlags{})
	assert.True(t, opts.HostSnapshot)
	assert.True(t, opts.BuildTimingSummary)
	assert.False(t, opts.BuildSettings)
}

func TestLoadEnrichmentOptions_envAll_enablesEveryPhase0(t *testing.T) {
	env := func(k string) string {
		if k == EnvEnrichmentAll {
			return "true"
		}

		return ""
	}

	opts := LoadEnrichmentOptions(nil, env, EnrichmentFlags{})
	assert.True(t, opts.HostSnapshot)
	assert.True(t, opts.BuildTimingSummary)
	assert.True(t, opts.BuildSettings)
}

func TestLoadEnrichmentOptions_envNone_overridesEnvAll(t *testing.T) {
	env := func(k string) string {
		switch k {
		case EnvEnrichmentAll:
			return "1"
		case EnvEnrichmentNone:
			return "yes"
		}

		return ""
	}

	opts := LoadEnrichmentOptions(nil, env, EnrichmentFlags{})
	assert.False(t, opts.AnyEnabled())
}

func TestLoadEnrichmentOptions_flagsWinOverEnv(t *testing.T) {
	env := func(k string) string {
		if k == EnvEnrichmentCollectors {
			return "hostsnap"
		}

		return ""
	}

	// Env turned hostsnap on; flags.All turns everything on too.
	opts := LoadEnrichmentOptions(nil, env, EnrichmentFlags{All: true})
	assert.True(t, opts.HostSnapshot)
	assert.True(t, opts.BuildTimingSummary)
	assert.True(t, opts.BuildSettings)
}

func TestLoadEnrichmentOptions_flagNone_killsEverything(t *testing.T) {
	env := func(k string) string {
		if k == EnvEnrichmentAll {
			return "true"
		}

		return ""
	}

	opts := LoadEnrichmentOptions(nil, env, EnrichmentFlags{None: true})
	assert.False(t, opts.AnyEnabled())
}

func TestLoadEnrichmentOptions_flagList_additive(t *testing.T) {
	// env sets hostsnap; flag adds buildsettings; result: both on.
	env := func(k string) string {
		if k == EnvEnrichmentCollectors {
			return "hostsnap"
		}

		return ""
	}

	opts := LoadEnrichmentOptions(nil, env, EnrichmentFlags{Enabled: []string{"buildsettings"}})
	assert.True(t, opts.HostSnapshot)
	assert.True(t, opts.BuildSettings)
	assert.False(t, opts.BuildTimingSummary)
}

func TestLoadEnrichmentOptions_envListUnknownToken_ignored(t *testing.T) {
	env := func(k string) string {
		if k == EnvEnrichmentCollectors {
			return "hostsnap,gradlemagic"
		}

		return ""
	}

	opts := LoadEnrichmentOptions(nil, env, EnrichmentFlags{})
	assert.True(t, opts.HostSnapshot)
	// unknown token must not panic / enable anything else
	assert.False(t, opts.BuildTimingSummary)
	assert.False(t, opts.BuildSettings)
}
