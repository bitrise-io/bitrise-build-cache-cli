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
	assert.Equal(t, enrichment.CommandClean, clean.Command(), "gerund form Cleaning -> clean")
}

func TestCommand_GerundForms(t *testing.T) {
	cases := map[string]enrichment.Command{
		"Build MyScheme":        enrichment.CommandBuild,
		"Building MyScheme":     enrichment.CommandBuild,
		"Clean MyScheme":        enrichment.CommandClean,
		"Cleaning MyScheme":     enrichment.CommandClean,
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
	assert.Equal(t, "build [S]", groups[0].Command())
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
	assert.Equal(t, "test [S]", groups[0].Command(), "Test outranks Build as primary")
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
	assert.Equal(t, "archive [S]", groups[0].Command(), "Archive outranks Build even when Build starts first")
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
	assert.Equal(t, "build [S]", groups[0].Command())
}

// Pins the wide-span trade-off documented on ManifestEntryGroup so nobody
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

func TestManifestEntryGroup_ErrorMessage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		statuses []string
		want     string
	}{
		{name: "empty", statuses: nil, want: ""},
		{name: "all success", statuses: []string{"S", "S"}, want: ""},
		{name: "single error", statuses: []string{"E"}, want: "xcodebuild failed (status=E)"},
		{name: "mixed picks first failing entry", statuses: []string{"S", "E", "E"}, want: "xcodebuild failed (status=E)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var entries []enrichment.ManifestEntry
			for _, s := range tc.statuses {
				entries = append(entries, enrichment.ManifestEntry{Status: s})
			}
			group := enrichment.ManifestEntryGroup{Entries: entries}
			assert.Equal(t, tc.want, group.ErrorMessage())
		})
	}
}

func TestLoadManifestGrouped_ThreeSchemesThreeGroups(t *testing.T) {
	groups, err := enrichment.LoadManifestGrouped("testdata/LogStoreManifest.plist", 60*time.Second)
	require.NoError(t, err)
	require.Len(t, groups, 3)
}

func TestManifestEntryGroup_CleanCommandRendering(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "c1", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base, Stop: base.Add(2 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.Equal(t, "clean [S]", groups[0].Command())
}

func TestPrimary_BuildBeatsCleanInMixedGroup(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "c", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base, Stop: base.Add(1 * time.Second)},
		{UUID: "b", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(5 * time.Second), Stop: base.Add(10 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.Equal(t, enrichment.CommandBuild, groups[0].Primary().Command(),
		"Build must outrank Clean even when Clean starts earlier — rank beats same-time tiebreak")
}

func TestPrimary_CleanOnlyGroupReportsClean(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "c1", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base, Stop: base.Add(2 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.Equal(t, enrichment.CommandClean, groups[0].Primary().Command())
}

func TestPrimary_CleanBeatsUnknown(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u", SchemeName: "S", Signature: "Resolve Packages", Status: "S", Start: base, Stop: base.Add(1 * time.Second)},
		{UUID: "c", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base.Add(2 * time.Second), Stop: base.Add(4 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries, 60*time.Second)
	require.Len(t, groups, 1)
	assert.Equal(t, enrichment.CommandClean, groups[0].Primary().Command(),
		"Clean must outrank Unknown so a Clean+Unknown group still PUTs with command=clean")
}

func TestSyntheticManifestGroup_PopulatesBuildCommandAndSpan(t *testing.T) {
	start := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	stop := start.Add(45 * time.Second)
	g := enrichment.SyntheticManifestGroup("abc-123", start, stop, "", "", "")

	require.Len(t, g.Entries, 1)
	assert.Equal(t, enrichment.SyntheticManifestGroupUUIDPrefix+"abc-123", g.Entries[0].UUID,
		"synthetic UUID must carry the sentinel prefix so it never collides with Xcode bare UUIDs")
	assert.Equal(t, enrichment.CommandBuild, g.Entries[0].Command(),
		"empty signature must fall back to \"build\" so the enricher does not drop the group")
	assert.Equal(t, start, g.Start())
	assert.Equal(t, stop, g.Stop())
	assert.Equal(t, "build", g.Command(), "FullCommand+scheme-less synthesis must land as a bare \"build\"")
	assert.Empty(t, g.OverrideInvocationID(), "no override stamped")
}

func TestSyntheticManifestGroup_WithSignatureAndSchemeRendersFullCommand(t *testing.T) {
	start := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	stop := start.Add(45 * time.Second)
	g := enrichment.SyntheticManifestGroup("abc-123", start, stop, "Build IceCubesApp", "IceCubesApp", "")

	require.Len(t, g.Entries, 1)
	assert.Equal(t, enrichment.CommandBuild, g.Entries[0].Command())
	assert.Equal(t, "build [IceCubesApp]", g.Command(),
		"signature+scheme must render as the real action+scheme command")
}

func TestSyntheticManifestGroup_StampsOverrideInvocationID(t *testing.T) {
	start := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	stop := start.Add(1 * time.Second)
	g := enrichment.SyntheticManifestGroup("sidecar-x", start, stop, "", "", "override-id")

	assert.Equal(t, "override-id", g.OverrideInvocationID())
}

// TestManifestEntryGroup_Primary_StableAcrossShuffledInput pins the UUID
// tie-break on Primary(). LoadManifest iterates the plist's Logs map in
// random Go-map order; the previous rank+Start tie-break would then return
// different Primary entries across scans of the same manifest, which breaks
// the "manifest UUID == BE InvocationID" convergence promise. Lex-UUID
// tie-break makes the choice deterministic regardless of input order.
func TestManifestEntryGroup_Primary_StableAcrossShuffledInput(t *testing.T) {
	base := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)

	// Three entries at identical rank (all build) + identical Start — only UUID
	// differs. Any Primary() tiebreak that doesn't consult UUID can land on
	// whichever entry appears first in the slice.
	entries := []enrichment.ManifestEntry{
		{UUID: "mmm-middle", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "aaa-first", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "zzz-last", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
	}

	orderings := [][]enrichment.ManifestEntry{
		{entries[0], entries[1], entries[2]},
		{entries[1], entries[2], entries[0]},
		{entries[2], entries[0], entries[1]},
		{entries[1], entries[0], entries[2]},
		{entries[2], entries[1], entries[0]},
		{entries[0], entries[2], entries[1]},
	}

	var picks []string
	for _, order := range orderings {
		g := enrichment.ManifestEntryGroup{Entries: append([]enrichment.ManifestEntry(nil), order...)}
		picks = append(picks, g.Primary().UUID)
	}

	for _, p := range picks {
		assert.Equal(t, "aaa-first", p,
			"Primary() must pick the lex-smallest UUID at tied rank+Start regardless of input order")
	}
}

// TestManifestEntryGroup_ManifestUUID_EmptyForSynthetic ensures the sentinel-
// prefixed pseudo-group UUID does not accidentally land as a BE InvocationID.
func TestManifestEntryGroup_ManifestUUID_EmptyForSynthetic(t *testing.T) {
	start := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	stop := start.Add(10 * time.Second)
	g := enrichment.SyntheticManifestGroup("sidecar-xyz", start, stop, "", "", "")

	assert.Empty(t, g.ManifestUUID(),
		"synthetic pseudo-group UUIDs carry the sentinel prefix; ManifestUUID must return empty so the enricher falls through to override/sidecar")
}

// TestManifestEntryGroup_ManifestUUID_ReturnsPrimaryForManifestSourced is the
// happy-path mirror of the synthetic-group check: real manifest-sourced groups
// expose Primary().UUID as the authoritative BE identity.
func TestManifestEntryGroup_ManifestUUID_ReturnsPrimaryForManifestSourced(t *testing.T) {
	base := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u-primary", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
	}}

	assert.Equal(t, "u-primary", g.ManifestUUID())
}
