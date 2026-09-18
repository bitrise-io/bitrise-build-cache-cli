//go:build unit

package enrichment_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestLoadManifest_ParsesEntries(t *testing.T) {
	entries, err := enrichment.LoadManifest("testdata/LogStoreManifest.plist")
	require.NoError(t, err)
	require.Len(t, entries, 3)

	byUUID := map[string]enrichment.ManifestEntry{}
	for _, e := range entries {
		byUUID[e.UUID] = e
	}

	build := byUUID["AAAA-BBBB-BUILD"]
	assert.Equal(t, "MyScheme", build.SchemeName)
	assert.Equal(t, "Build MyScheme project", build.Signature)
	assert.Equal(t, "S", build.Status)
	assert.True(t, build.Success())
	assert.Equal(t, enrichment.CommandBuild, build.Command())
	assert.Equal(t, "AAAA-BBBB-BUILD.xcactivitylog", build.FileName)
	assert.Equal(t, time.Date(2025, 2, 27, 10, 41, 18, 500_000_000, time.UTC), build.Start.UTC())
	assert.WithinDuration(t, build.Start.Add(10*time.Second+250*time.Millisecond), build.Stop, time.Millisecond)

	test := byUUID["CCCC-DDDD-TEST"]
	assert.Equal(t, "MySchemeTests", test.SchemeName)
	assert.False(t, test.Success())
	assert.Equal(t, enrichment.CommandTest, test.Command())

	clean := byUUID["EEEE-FFFF-CLEAN"]
	assert.Equal(t, "S", clean.Status, "highLevelStatus must be read from primaryObservable")
	assert.True(t, clean.Success())
	assert.Equal(t, enrichment.CommandBuild, clean.Command(), "gerund form Cleaning -> build")
}

func TestCommand_GerundForms(t *testing.T) {
	cases := map[string]enrichment.Command{
		"Build MyScheme":        enrichment.CommandBuild,
		"Building MyScheme":     enrichment.CommandBuild,
		"Cleaning MyScheme":     enrichment.CommandBuild,
		"Test MySchemeTests":    enrichment.CommandTest,
		"Testing MySchemeTests": enrichment.CommandTest,
		"Archive MyScheme":      enrichment.CommandArchive,
		"Archiving MyScheme":    enrichment.CommandArchive,
		"Analyzing MyScheme":    enrichment.CommandUnknown,
	}
	for sig, want := range cases {
		got := enrichment.ManifestEntry{Signature: sig}.Command()
		assert.Equal(t, want, got, sig)
	}
}

func TestLoadManifest_Missing(t *testing.T) {
	_, err := enrichment.LoadManifest("testdata/does-not-exist.plist")
	require.Error(t, err)
}

// A green xcodebuild whose manifest status is not "S" used to enrich as failed.
func TestManifestEntry_SuccessOnlyFailsOnExplicitError(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{status: "S", want: true},
		{status: "W", want: true},
		{status: "", want: true},
		{status: "X", want: true},
		{status: "E", want: false},
	} {
		t.Run("status="+tc.status, func(t *testing.T) {
			assert.Equal(t, tc.want, enrichment.ManifestEntry{Status: tc.status}.Success())
		})
	}
}

func TestGroupManifestEntries_SingleEntry(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.Equal(t, []string{"u1"}, groups[0].UUIDs())
	assert.Equal(t, "S", groups[0].SchemeName())
	assert.Equal(t, 10*time.Second, groups[0].Duration())
	assert.True(t, groups[0].Success())
	assert.Equal(t, "build S", groups[0].Command())
	assert.Equal(t, "Build S", groups[0].FullCommand())
}

func TestGroupManifestEntries_MultiEntrySameSchemeWithinGap(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Test S", Status: "S", Start: base.Add(20 * time.Second), Stop: base.Add(40 * time.Second)},
		{UUID: "u3", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(50 * time.Second), Stop: base.Add(55 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.ElementsMatch(t, []string{"u1", "u2", "u3"}, groups[0].UUIDs())
	assert.Equal(t, base, groups[0].Start())
	assert.Equal(t, base.Add(55*time.Second), groups[0].Stop())
	assert.Equal(t, 55*time.Second, groups[0].Duration())
	assert.Equal(t, "test S", groups[0].Command(), "Test outranks Build as primary")
	assert.Equal(t, "Test S", groups[0].FullCommand())
	assert.True(t, groups[0].Success())
}

func TestGroupManifestEntries_TwoSchemesTwoGroups(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "a1", SchemeName: "A", Signature: "Build A", Status: "S", Start: base, Stop: base.Add(5 * time.Second)},
		{UUID: "b1", SchemeName: "B", Signature: "Build B", Status: "S", Start: base.Add(1 * time.Second), Stop: base.Add(6 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 2)

	byScheme := map[string]enrichment.ManifestEntryGroup{}
	for _, g := range groups {
		byScheme[g.SchemeName()] = g
	}
	assert.Equal(t, []string{"a1"}, byScheme["A"].UUIDs())
	assert.Equal(t, []string{"b1"}, byScheme["B"].UUIDs())
}

func TestGroupManifestEntries_SameSchemeSeparatedByGap(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(10 * time.Minute), Stop: base.Add(10*time.Minute + 5*time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 2)
	assert.Equal(t, []string{"u1"}, groups[0].UUIDs())
	assert.Equal(t, []string{"u2"}, groups[1].UUIDs())
}

func TestGroupManifestEntries_MixedSuccessFailsGroup(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Test S", Status: "E", Start: base.Add(15 * time.Second), Stop: base.Add(25 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.False(t, groups[0].Success(), "group Success is AND across entries")
}

func TestGroupManifestEntries_PrimaryOrdering(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "b", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(1 * time.Second), Stop: base.Add(5 * time.Second)},
		{UUID: "a", SchemeName: "S", Signature: "Archive S", Status: "S", Start: base.Add(2 * time.Second), Stop: base.Add(6 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.Equal(t, "archive S", groups[0].Command(), "Archive outranks Build even when Build starts first")
}

func TestGroupManifestEntries_SameRankBreaksByEarliestStart(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "later", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(5 * time.Second), Stop: base.Add(10 * time.Second)},
		{UUID: "earlier", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(1 * time.Second), Stop: base.Add(4 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.Equal(t, "earlier", groups[0].Primary().UUID, "same-rank primary ties break to earliest Start")
}

func TestGroupManifestEntries_HigherRankWinsOverUnknown(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Resolve Packages", Status: "S", Start: base, Stop: base.Add(1 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(2 * time.Second), Stop: base.Add(10 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.Equal(t, "build S", groups[0].Command())
}

// A wide burst-plus-late aggregate span (min-Start .. max-Stop) is intentional
// per the ManifestEntryGroup doc: GroupCorrelationSpan reports the whole span,
// which lets overlap-based correlation match a pending record that only
// overlaps part of the window. Documenting the trade-off here so nobody
// "fixes" the aggregation by narrowing the span later.
func TestGroupCorrelationSpan_WideAggregateSpanCanFalseMatchCorrelate(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "old", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(2 * time.Second)},
		{UUID: "new", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(40 * time.Second), Stop: base.Add(50 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, enrichment.LocalGroupTimeGap)
	require.Len(t, groups, 1, "entries within LocalGroupTimeGap must collapse into one group")

	group := groups[0]
	assert.Equal(t, base, group.Start(), "aggregate Start is the earliest entry")
	assert.Equal(t, base.Add(50*time.Second), group.Stop(), "aggregate Stop is the latest entry")

	span := enrichment.GroupCorrelationSpan(group)
	assert.Equal(t, base, span.Start, "correlation span is the aggregate min-Start")
	assert.Equal(t, base.Add(50*time.Second), span.Stop, "correlation span is the aggregate max-Stop")

	// A pending record that only touches the first-entry burst [base, base+30s]
	// still overlaps the wide span, so Correlate returns a match.
	pending := []enrichment.PendingRecord{
		{
			InvocationID: "burst-only",
			StartTime:    base,
			Duration:     int64(30 * time.Second / time.Millisecond),
		},
	}
	id, matched := enrichment.Correlate(span, pending)
	assert.True(t, matched, "wide span overlaps the burst-only pending record")
	assert.Equal(t, "burst-only", id, "the sole pending record wins the overlap")
}

func TestLoadManifestGrouped_ThreeSchemesThreeGroups(t *testing.T) {
	groups, err := enrichment.LoadManifestGrouped("testdata/LogStoreManifest.plist", 60*time.Second)
	require.NoError(t, err)
	// Fixture has three schemes (MyScheme, MySchemeTests, Models) — three groups.
	require.Len(t, groups, 3)
}
