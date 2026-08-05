//go:build unit

package refresh

import (
	"bytes"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

func TestNeedsNudge_majorBump(t *testing.T) {
	cases := []struct {
		stored, current string
		want            bool
	}{
		{"1.0.0", "2.0.0", true},   // major bump → nudge
		{"1.5.3", "2.0.0", true},   // same
		{"1.0.0", "1.5.0", false},  // minor only → silent
		{"2.0.0", "1.0.0", false},  // local-ahead / rollback → silent
		{"", "1.0.0", false},       // empty stored treated as v1.0.0 baseline → silent on first major
		{"", "2.0.0", true},        // unversioned config behind v2 → nudge
		{"not-semver", "2.0.0", true},
		{"1.0.0", "garbage", false}, // invalid current → silent
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, needsNudge(tc.stored, tc.current),
			"stored=%q current=%q", tc.stored, tc.current)
	}
}

func TestNotify_writesPerStaleTool(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&buf))

	samples := []toolconfig.Sample{
		{Tool: toolconfig.Gradle, ConfigVersion: toolconfig.GradleConfigVersion},
		{Tool: toolconfig.Bazel, ConfigVersion: "0.0.0"}, // pretend major behind
	}

	// Force bazel current to v1 so 0.0.0 < 1.0.0 triggers nudge.
	Notify(logger, samples)

	out := buf.String()
	// Only the stale (bazel) tool should appear:
	assert.NotContains(t, out, "activate gradle")
	// Bazel was at "0.0.0", below the v1 baseline:
	assert.Contains(t, out, "activate bazel")
}

func TestNotify_silentWhenAllCurrent(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&buf))

	Notify(logger, []toolconfig.Sample{
		{Tool: toolconfig.Gradle, ConfigVersion: toolconfig.GradleConfigVersion},
		{Tool: toolconfig.Bazel, ConfigVersion: toolconfig.BazelConfigVersion},
	})

	assert.Empty(t, buf.String())
}

func TestNotify_nilLoggerOrEmptyIsNoOp(t *testing.T) {
	Notify(nil, []toolconfig.Sample{{Tool: toolconfig.Gradle}})

	var buf bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&buf))
	Notify(logger, nil)
	assert.Empty(t, buf.String())
}
