//go:build unit

package invocationlog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func TestDefault_WritesViaInvocationsWriter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	appender := Default(nil)
	require.NotNil(t, appender)

	err := appender.Append(invocations.Record{
		InvocationID: "test",
		Command:      "xcodebuild",
		Tool:         invocations.ToolXcode,
		StartedAt:    time.Unix(1_700_000_000, 0).UTC(),
	})
	require.NoError(t, err)

	// Reader hits the same path.
	p, err := paths.Default()
	require.NoError(t, err)
	r := invocations.NewReader(p)
	got, err := r.Recent(10)
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "test", got[0].InvocationID)
}

func TestAppend_IsCallerConvenience(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	err := Append(nil, invocations.Record{
		InvocationID: "convenience",
		Command:      "xcodebuild",
		Tool:         invocations.ToolXcode,
		StartedAt:    time.Unix(1_700_000_000, 0).UTC(),
	})
	require.NoError(t, err)
}
