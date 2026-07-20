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
		InvocationID: "loc-1",
		Command:      "xcodebuild build",
		Tool:         invpkg.ToolXcode,
		CLIVersion:   "v3.0.0",
		StartedAt:    day,
		FinishedAt:   day.Add(30 * time.Second),
		ExitCode:     0,
		HitRate:      0.5,
	}))
	require.NoError(t, w.Append(invpkg.Record{
		InvocationID: "loc-fail",
		Command:      "xcodebuild test",
		Tool:         invpkg.ToolXcode,
		CLIVersion:   "v3.0.0",
		StartedAt:    day.Add(time.Minute),
		FinishedAt:   day.Add(time.Minute + 10*time.Second),
		ExitCode:     65,
	}))
	require.NoError(t, w.Append(invpkg.Record{
		InvocationID: "ci-1",
		Command:      "xcodebuild build",
		Tool:         invpkg.ToolXcode,
		CLIVersion:   "v3.0.0",
		StartedAt:    day.Add(2 * time.Minute),
		FinishedAt:   day.Add(2*time.Minute + 20*time.Second),
		ExitCode:     0,
		CIProvider:   "bitrise",
		HitRate:      0.9,
	}))

	return p
}

func runList(t *testing.T, p paths.Paths, source string, jsonOut bool) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	prev := listFlags
	listFlags.limit = 10
	listFlags.source = source
	listFlags.json = jsonOut

	t.Cleanup(func() { listFlags = prev })

	match, err := matcherFor(source)
	require.NoError(t, err)

	records, err := invpkg.NewReader(p).RecentMatching(listFlags.limit, match)
	require.NoError(t, err)

	if jsonOut {
		require.NoError(t, writeJSON(buf, records))
	} else {
		require.NoError(t, writeTable(buf, records))
	}

	return buf
}

func TestList_defaultSourceIsLocal(t *testing.T) {
	p := seedRecords(t)

	buf := runList(t, p, sourceLocal, false)
	out := buf.String()

	assert.Contains(t, out, "loc-1")
	assert.Contains(t, out, "loc-fail")
	assert.NotContains(t, out, "ci-1")
	assert.Contains(t, out, "50%")
	assert.Contains(t, out, "success")
	assert.Contains(t, out, "failed")
}

func TestList_sourceCIOnlyReturnsCIRecords(t *testing.T) {
	p := seedRecords(t)

	buf := runList(t, p, sourceCI, false)
	out := buf.String()

	assert.Contains(t, out, "ci-1")
	assert.NotContains(t, out, "loc-1")
	assert.Contains(t, out, "90%")
}

func TestList_sourceAllReturnsBoth(t *testing.T) {
	p := seedRecords(t)

	buf := runList(t, p, sourceAll, false)
	out := buf.String()

	assert.Contains(t, out, "loc-1")
	assert.Contains(t, out, "loc-fail")
	assert.Contains(t, out, "ci-1")
}

func TestList_jsonOutputRoundtrips(t *testing.T) {
	p := seedRecords(t)

	buf := runList(t, p, sourceAll, true)

	var got []invpkg.Record
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Len(t, got, 3)
}

func TestList_jsonEmptyIsEmptyArray(t *testing.T) {
	p := paths.FromHome(t.TempDir())

	buf := runList(t, p, sourceLocal, true)
	assert.Equal(t, "[]\n", buf.String())
}

func TestList_tableRendersMissingHitRateAsDash(t *testing.T) {
	p := seedRecords(t)

	buf := runList(t, p, sourceLocal, false)
	lines := strings.Split(buf.String(), "\n")

	var failedFields []string
	for _, ln := range lines {
		if strings.Contains(ln, "loc-fail") {
			for _, field := range strings.Split(ln, "  ") {
				trimmed := strings.TrimSpace(field)
				if trimmed != "" {
					failedFields = append(failedFields, trimmed)
				}
			}

			break
		}
	}
	require.NotEmpty(t, failedFields, "expected a row for loc-fail")
	assert.Contains(t, failedFields, "-", "hit rate column should render as '-' when unset")
	assert.Contains(t, failedFields, "failed")
}

func TestList_endToEndViaRunE(t *testing.T) {
	p := seedRecords(t)

	t.Setenv("HOME", p.Home)

	prev := listFlags
	listFlags.limit = 10
	listFlags.source = sourceLocal
	listFlags.json = true

	t.Cleanup(func() { listFlags = prev })

	buf := &bytes.Buffer{}
	listCmd.SetOut(buf)
	listCmd.SetErr(buf)

	require.NoError(t, listCmd.RunE(listCmd, nil))

	var got []invpkg.Record
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Len(t, got, 2, "should include both local records via paths.Default() resolution")
}

func TestMatcherFor_rejectsUnknownSource(t *testing.T) {
	_, err := matcherFor("bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}
