//go:build unit

package invocations

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	invpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func seedRecords(t *testing.T) paths.Paths {
	t.Helper()

	p := paths.FromHome(t.TempDir())
	day := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	w := invpkg.NewWriter(p)
	w.Clock = func() time.Time { return day }

	require.NoError(t, w.Append(invpkg.Record{
		InvocationID: "inv-1",
		Command:      "xcodebuild build",
		Tool:         invpkg.ToolXcode,
		CLIVersion:   "v3.0.0",
		StartedAt:    day,
		FinishedAt:   day.Add(30 * time.Second),
		ExitCode:     0,
		HitRate:      0.5,
	}))
	require.NoError(t, w.Append(invpkg.Record{
		InvocationID: "inv-fail",
		Command:      "xcodebuild test",
		Tool:         invpkg.ToolXcode,
		CLIVersion:   "v3.0.0",
		StartedAt:    day.Add(time.Minute),
		FinishedAt:   day.Add(time.Minute + 10*time.Second),
		ExitCode:     65,
	}))

	return p
}

func runList(t *testing.T, p paths.Paths, jsonOut bool) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	prev := listFlags
	listFlags.limit = 10
	listFlags.json = jsonOut

	t.Cleanup(func() { listFlags = prev })

	records, err := invpkg.NewReader(p).Recent(listFlags.limit)
	require.NoError(t, err)

	if jsonOut {
		require.NoError(t, writeJSON(buf, records))
	} else {
		require.NoError(t, writeTable(buf, records))
	}

	return buf
}

func TestList_tableIncludesAllRecords(t *testing.T) {
	p := seedRecords(t)

	out := runList(t, p, false).String()

	assert.Contains(t, out, "inv-1")
	assert.Contains(t, out, "inv-fail")
	assert.Contains(t, out, "50%")
	assert.Contains(t, out, "success")
	assert.Contains(t, out, "failed")
}

func TestList_jsonOutputRoundtrips(t *testing.T) {
	p := seedRecords(t)

	var got []invpkg.Record
	require.NoError(t, json.Unmarshal(runList(t, p, true).Bytes(), &got))
	assert.Len(t, got, 2)
}

func TestList_jsonEmptyIsEmptyArray(t *testing.T) {
	assert.Equal(t, "[]\n", runList(t, paths.FromHome(t.TempDir()), true).String())
}

func TestList_tableRendersMissingHitRateAsDash(t *testing.T) {
	p := seedRecords(t)

	lines := strings.Split(runList(t, p, false).String(), "\n")

	var failedFields []string
	for _, ln := range lines {
		if strings.Contains(ln, "inv-fail") {
			for _, field := range strings.Split(ln, "  ") {
				trimmed := strings.TrimSpace(field)
				if trimmed != "" {
					failedFields = append(failedFields, trimmed)
				}
			}

			break
		}
	}
	require.NotEmpty(t, failedFields, "expected a row for inv-fail")
	assert.Contains(t, failedFields, "-", "hit rate column should render as '-' when unset")
	assert.Contains(t, failedFields, "failed")
}

func TestList_endToEndViaRunE(t *testing.T) {
	p := seedRecords(t)

	t.Setenv("HOME", p.Home)

	prev := listFlags
	listFlags.limit = 10
	listFlags.json = true

	t.Cleanup(func() { listFlags = prev })

	buf := &bytes.Buffer{}
	listCmd.SetOut(buf)
	listCmd.SetErr(buf)

	require.NoError(t, listCmd.RunE(listCmd, nil))

	var got []invpkg.Record
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Len(t, got, 2, "paths.Default() should resolve to the seeded HOME")
}
