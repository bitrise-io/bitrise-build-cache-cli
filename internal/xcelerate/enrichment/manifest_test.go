//go:build unit

package enrichment_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

// hashLookup builds a MetricsLookup that returns a canned BuildDescriptionHash
// per entry.UUID. Entries whose UUID is absent from the map resolve to an
// empty hash (OutcomeOK — present-but-no-hash is a valid Xcode state that
// must NOT split).
func hashLookup(byUUID map[string]string) enrichment.MetricsLookup {
	return func(entry enrichment.ManifestEntry) xcactivitylog.Metrics {
		return xcactivitylog.Metrics{
			Outcome:              xcactivitylog.OutcomeOK,
			BuildDescriptionHash: byUUID[entry.UUID],
		}
	}
}

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

	groups := enrichment.GroupManifestEntries(entries)
	require.Len(t, groups, 1)
	assert.Equal(t, []string{"u1"}, groups[0].UUIDs())
	assert.Equal(t, "S", groups[0].SchemeName())
	assert.Equal(t, 10*time.Second, groups[0].Duration())
	assert.True(t, groups[0].Success())
	assert.Equal(t, "build [S]", groups[0].Command())
	assert.Equal(t, "xcodebuild build -scheme S", groups[0].FullCommand())
}

func TestGroupManifestEntries_MultiEntrySameScheme_DistinctActionsSplit(t *testing.T) {
	// Each distinct user action (Build / Test / Build) lands as its own BE row.
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Test S", Status: "S", Start: base.Add(20 * time.Second), Stop: base.Add(40 * time.Second)},
		{UUID: "u3", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(50 * time.Second), Stop: base.Add(55 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries)
	require.Len(t, groups, 3, "three distinct actions produce three groups")
	assert.Equal(t, []string{"u1"}, groups[0].UUIDs())
	assert.Equal(t, []string{"u2"}, groups[1].UUIDs())
	assert.Equal(t, []string{"u3"}, groups[2].UUIDs())
}

func TestGroupManifestEntries_TwoSchemesTwoGroups(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "a1", SchemeName: "A", Signature: "Build A", Status: "S", Start: base, Stop: base.Add(5 * time.Second)},
		{UUID: "b1", SchemeName: "B", Signature: "Build B", Status: "S", Start: base.Add(1 * time.Second), Stop: base.Add(6 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries)
	require.Len(t, groups, 2)

	byScheme := map[string]enrichment.ManifestEntryGroup{}
	for _, g := range groups {
		byScheme[g.SchemeName()] = g
	}
	assert.Equal(t, []string{"a1"}, byScheme["A"].UUIDs())
	assert.Equal(t, []string{"b1"}, byScheme["B"].UUIDs())
}

func TestGroupManifestEntries_SameSchemeSeparatedByGap(t *testing.T) {
	// Two Builds minutes apart — each is its own BE row regardless of hex.
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(10 * time.Minute), Stop: base.Add(10*time.Minute + 5*time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries)
	require.Len(t, groups, 2)
	assert.Equal(t, []string{"u1"}, groups[0].UUIDs())
	assert.Equal(t, []string{"u2"}, groups[1].UUIDs())
}

func TestGroupManifestEntries_MixedSuccessFailsGroup(t *testing.T) {
	// Distinct Build + Test actions split; each row stands on its own Success flag.
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Test S", Status: "E", Start: base.Add(15 * time.Second), Stop: base.Add(25 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries)
	require.Len(t, groups, 2)
	assert.True(t, groups[0].Success(), "Build row stays green")
	assert.False(t, groups[1].Success(), "Test row surfaces its own failure")
}

// build-for-testing + test share a scheme and would once cluster into one
// group; the Primary() picker must still rank Build over Test so hit rate is
// attributed to the build log. Tested directly on a hand-built group.
func TestPrimary_PrefersBuildForTestingOverTest_InSameSchemeGroup(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "build-for-testing", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(30 * time.Second)},
		{UUID: "test-without-building", SchemeName: "S", Signature: "Test S", Status: "S", Start: base.Add(35 * time.Second), Stop: base.Add(60 * time.Second)},
	}}

	assert.Equal(t, "build-for-testing", g.Primary().UUID, "Build must outrank Test so hit rate reads from the build log")
	assert.Equal(t, enrichment.CommandBuild, g.Primary().Command())
}

func TestPrimary_ArchiveOutranksBuild(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "b", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(1 * time.Second), Stop: base.Add(5 * time.Second)},
		{UUID: "a", SchemeName: "S", Signature: "Archive S", Status: "S", Start: base.Add(2 * time.Second), Stop: base.Add(6 * time.Second)},
	}}

	assert.Equal(t, "archive [S]", g.Command(), "Archive outranks Build as primary")
}

func TestPrimary_SameRankBreaksByEarliestStart(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "later", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(5 * time.Second), Stop: base.Add(10 * time.Second)},
		{UUID: "earlier", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(1 * time.Second), Stop: base.Add(4 * time.Second)},
	}}

	assert.Equal(t, "earlier", g.Primary().UUID, "same-rank primary ties break to earliest Start")
}

func TestPrimary_HigherRankWinsOverUnknown(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Resolve Packages", Status: "S", Start: base, Stop: base.Add(1 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(2 * time.Second), Stop: base.Add(10 * time.Second)},
	}}

	assert.Equal(t, "build [S]", g.Command())
}

// Pins the wide-aggregate-span trade-off documented on ManifestEntryGroup so
// nobody "fixes" the aggregation by narrowing the span later. A prep+real
// Build pair within mergeMaxGap collapses into one group whose Start..Stop
// spans both, and Correlate's overlap matcher will happily match a pending
// record that only overlaps part of the window.
func TestGroupCorrelationSpan_WideAggregateSpanCanFalseMatchCorrelate(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "prep", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(2 * time.Second)},
		{UUID: "real", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(4 * time.Second), Stop: base.Add(50 * time.Second)},
	}

	lookup := hashLookup(map[string]string{"prep": "abcd1234", "real": "abcd1234"})

	groups := enrichment.GroupManifestEntries(entries, enrichment.WithMetricsLookup(lookup))
	require.Len(t, groups, 1, "prep+real Build (same scheme/Command/hex, <3s apart) merge into one group")

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
	groups, err := enrichment.LoadManifestGrouped("testdata/LogStoreManifest.plist")
	require.NoError(t, err)
	require.Len(t, groups, 3)
}

// `\b`-anchored regex must not swallow MyProject / MyConfiguration as a trailing
// substring of a word (e.g. "Build MyProject" or "Running MyConfiguration").
func TestProject_IgnoresSubstringMatch(t *testing.T) {
	for _, sig := range []string{
		"Build workspace MyProject with scheme Foo",
		"Building MyProject target with scheme Foo",
	} {
		assert.Empty(t, enrichment.ManifestEntry{Signature: sig}.Project(), sig)
	}
}

func TestConfiguration_IgnoresSubstringMatch(t *testing.T) {
	for _, sig := range []string{
		"Running MyConfiguration with Foo",
		"Build using TheirConfiguration setup",
	} {
		assert.Empty(t, enrichment.ManifestEntry{Signature: sig}.Configuration(), sig)
	}
}

// Positive confirmation that the bare xcodebuild-signature shape still parses
// (matches testdata/LogStoreManifest.plist "Cleaning project ... configuration Debug").
func TestProjectAndConfiguration_ParseBareSignature(t *testing.T) {
	m := enrichment.ManifestEntry{
		Signature: "Cleaning project IceCubesApp with scheme Models and configuration Debug",
	}
	assert.Equal(t, "IceCubesApp", m.Project())
	assert.Equal(t, "Debug", m.Configuration())
}

func TestWorkspace_ParsesWorkspaceSignature(t *testing.T) {
	m := enrichment.ManifestEntry{
		Signature: "Building workspace ComposableArchitecture with scheme ComposableArchitecture",
	}
	assert.Equal(t, "ComposableArchitecture", m.Workspace())
	assert.Empty(t, m.Project())
	assert.Empty(t, m.Configuration())
}

func TestFullCommand_WorkspaceSignatureRendersWorkspaceFlag(t *testing.T) {
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{{
		SchemeName: "ComposableArchitecture",
		Signature:  "Building workspace ComposableArchitecture with scheme ComposableArchitecture",
	}}}
	assert.Equal(t, "xcodebuild build -workspace ComposableArchitecture -scheme ComposableArchitecture", g.FullCommand())
}

func TestFullCommand_ProjectSignatureRendersProjectFlag(t *testing.T) {
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{{
		SchemeName: "Models",
		Signature:  "Cleaning project IceCubesApp with scheme Models and configuration Debug",
	}}}
	assert.Equal(t, "xcodebuild clean -project IceCubesApp -scheme Models -configuration Debug", g.FullCommand())
}

func TestFullCommand_UnknownSignatureFallsBackVerbatim(t *testing.T) {
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{{
		Signature: "Preparing build description",
	}}}
	assert.Equal(t, "Preparing build description", g.FullCommand())
}

func TestManifestEntryGroup_CleanCommandRendering(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "c1", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base, Stop: base.Add(2 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries)
	require.Len(t, groups, 1)
	assert.Equal(t, "clean [S]", groups[0].Command())
}

func TestPrimary_BuildBeatsCleanInMixedGroup(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "c", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base, Stop: base.Add(1 * time.Second)},
		{UUID: "b", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(5 * time.Second), Stop: base.Add(10 * time.Second)},
	}}

	assert.Equal(t, enrichment.CommandBuild, g.Primary().Command(),
		"Build must outrank Clean even when Clean starts earlier — rank beats same-time tiebreak")
}

func TestPrimary_CleanOnlyGroupReportsClean(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "c1", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base, Stop: base.Add(2 * time.Second)},
	}

	groups := enrichment.GroupManifestEntries(entries)
	require.Len(t, groups, 1)
	assert.Equal(t, enrichment.CommandClean, groups[0].Primary().Command())
}

func TestPrimary_CleanBeatsUnknown(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u", SchemeName: "S", Signature: "Resolve Packages", Status: "S", Start: base, Stop: base.Add(1 * time.Second)},
		{UUID: "c", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base.Add(2 * time.Second), Stop: base.Add(4 * time.Second)},
	}}

	assert.Equal(t, enrichment.CommandClean, g.Primary().Command(),
		"Clean must outrank Unknown so a Clean+Unknown group still PUTs with command=clean")
}

func TestSyntheticManifestGroup_PopulatesBuildCommandAndSpan(t *testing.T) {
	start := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	stop := start.Add(45 * time.Second)
	g := enrichment.SyntheticManifestGroup("abc-123", start, stop, "", "", "", enrichment.SyntheticClassification{})

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
	g := enrichment.SyntheticManifestGroup("abc-123", start, stop, "Build IceCubesApp", "IceCubesApp", "", enrichment.SyntheticClassification{})

	require.Len(t, g.Entries, 1)
	assert.Equal(t, enrichment.CommandBuild, g.Entries[0].Command())
	assert.Equal(t, "build [IceCubesApp]", g.Command(),
		"signature+scheme must render as the real action+scheme command")
}

func TestSyntheticManifestGroup_StampsOverrideInvocationID(t *testing.T) {
	start := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	stop := start.Add(1 * time.Second)
	g := enrichment.SyntheticManifestGroup("sidecar-x", start, stop, "", "", "override-id", enrichment.SyntheticClassification{})

	assert.Equal(t, "override-id", g.OverrideInvocationID())
}

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
	g := enrichment.SyntheticManifestGroup("sidecar-xyz", start, stop, "", "", "", enrichment.SyntheticClassification{})

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

// TestManifestEntryGroup_PrimarySpan_UsesPrimaryEntryOnly guards the split
// between the row-side (PrimarySpan — anchor's own window) and correlation-side
// (Duration — wide cluster span including post-build indexing / preview compiles).
func TestManifestEntryGroup_PrimarySpan_UsesPrimaryEntryOnly(t *testing.T) {
	base := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		// Primary: Build (rank 1), 10s window.
		{UUID: "u-primary", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		// Late non-Primary: unknown command (rank -1), 50s window joining the cluster.
		{UUID: "u-late", SchemeName: "S", Signature: "Indexing S", Status: "S", Start: base.Add(20 * time.Second), Stop: base.Add(70 * time.Second)},
	}}

	assert.Equal(t, 10*time.Second, g.PrimarySpan(), "PrimarySpan is anchored to Primary().Stop-Primary().Start")
	assert.Equal(t, 70*time.Second, g.Duration(), "Duration stays as the wide cluster span used by correlation")
}

func TestFullCommand_SchemeOnlyNoWorkspaceOrProject(t *testing.T) {
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{{
		SchemeName: "S",
		Signature:  "Build S",
	}}}

	assert.Equal(t, "xcodebuild build -scheme S", g.FullCommand(),
		"a known action is enough to render the xcodebuild argv form even without workspace/project/configuration")
}

// A prep-phase Build and its real-Build companion share scheme + Command +
// xcbuilddata hash and land within mergeMaxGap — the one case the merge rule
// collapses into a single BE row.
func TestGroupManifestEntries_PrepPlusRealBuildSameHexSubSecondGap_Merges(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "prep", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(500 * time.Millisecond)},
		{UUID: "real", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(1 * time.Second), Stop: base.Add(40 * time.Second)},
	}

	lookup := hashLookup(map[string]string{"prep": "abcd1234", "real": "abcd1234"})

	groups := enrichment.GroupManifestEntries(entries, enrichment.WithMetricsLookup(lookup))
	require.Len(t, groups, 1, "same scheme/Command/hex + sub-second gap must merge — prep-Build + real-Build is one BE row")
	assert.ElementsMatch(t, []string{"prep", "real"}, groups[0].UUIDs())
}

// v7 fix: two ⌘Bs on the same xcbuilddata hash but >=3s apart no longer merge.
func TestGroupManifestEntries_RapidConsecutiveSameHex_Splits(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "build1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(5 * time.Second)},
		{UUID: "build2", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(10 * time.Second), Stop: base.Add(15 * time.Second)},
	}

	lookup := hashLookup(map[string]string{"build1": "abcd1234", "build2": "abcd1234"})

	groups := enrichment.GroupManifestEntries(entries, enrichment.WithMetricsLookup(lookup))
	require.Len(t, groups, 2, "same hex but ≥3s apart → two user actions → two BE rows")
	assert.Equal(t, []string{"build1"}, groups[0].UUIDs())
	assert.Equal(t, []string{"build2"}, groups[1].UUIDs())
}

// Clean + Build share a scheme and hex but differ in Command — two user
// actions, two BE rows.
func TestGroupManifestEntries_CleanAndBuildSameHash_Splits(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "clean", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base, Stop: base.Add(500 * time.Millisecond)},
		{UUID: "build", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(1 * time.Second), Stop: base.Add(40 * time.Second)},
	}

	lookup := hashLookup(map[string]string{"clean": "abcd1234", "build": "abcd1234"})

	groups := enrichment.GroupManifestEntries(entries, enrichment.WithMetricsLookup(lookup))
	require.Len(t, groups, 2)
	assert.Equal(t, []string{"clean"}, groups[0].UUIDs())
	assert.Equal(t, []string{"build"}, groups[1].UUIDs())
}

// v6 RDE repro: two consecutive ⌘Bs with scheme=Models within a minute
// previously merged into one 2-UUID group whose Primary.UUID shadowed the
// second ⌘B's PUT. Distinct build-description hashes must split.
func TestGroupManifestEntries_DifferentHex_Splits(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "build1", SchemeName: "Models", Signature: "Build Models", Status: "S", Start: base, Stop: base.Add(15 * time.Second)},
		{UUID: "build2", SchemeName: "Models", Signature: "Build Models", Status: "S", Start: base.Add(20 * time.Second), Stop: base.Add(45 * time.Second)},
	}

	lookup := hashLookup(map[string]string{"build1": "aaaa1111", "build2": "bbbb2222"})

	groups := enrichment.GroupManifestEntries(entries, enrichment.WithMetricsLookup(lookup))
	require.Len(t, groups, 2, "distinct xcbuilddata hashes split; both ⌘Bs land as their own BE row")
	assert.Equal(t, []string{"build1"}, groups[0].UUIDs())
	assert.Equal(t, []string{"build2"}, groups[1].UUIDs())
}

// Empty hash on either side blocks the merge — the safe default.
func TestGroupManifestEntries_EmptyHex_AlwaysSplits(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(1 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(2 * time.Second), Stop: base.Add(3 * time.Second)},
	}

	// Both lookups return empty hash — reader miss / unparsed log.
	lookup := func(_ enrichment.ManifestEntry) xcactivitylog.Metrics {
		return xcactivitylog.Metrics{Outcome: xcactivitylog.OutcomeUnparsed}
	}

	groups := enrichment.GroupManifestEntries(entries, enrichment.WithMetricsLookup(lookup))
	require.Len(t, groups, 2, "no hex on either side → merge rule declines → each entry stands alone")
	assert.Equal(t, []string{"u1"}, groups[0].UUIDs())
	assert.Equal(t, []string{"u2"}, groups[1].UUIDs())
}

// v7 reproducer: three ⌘Bs 10 s apart on the same xcbuilddata hash land as
// three independent BE rows, not one merged burst.
func TestGroupManifestEntries_ThreeCommandBsSameHash_SplitsIntoThree(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "b1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(2 * time.Second)},
		{UUID: "b2", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(12 * time.Second), Stop: base.Add(14 * time.Second)},
		{UUID: "b3", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(24 * time.Second), Stop: base.Add(26 * time.Second)},
	}

	lookup := hashLookup(map[string]string{"b1": "deadbeef", "b2": "deadbeef", "b3": "deadbeef"})

	groups := enrichment.GroupManifestEntries(entries, enrichment.WithMetricsLookup(lookup))
	require.Len(t, groups, 3, "three ⌘Bs 10 s apart → three BE rows even on identical hex")
	assert.Equal(t, []string{"b1"}, groups[0].UUIDs())
	assert.Equal(t, []string{"b2"}, groups[1].UUIDs())
	assert.Equal(t, []string{"b3"}, groups[2].UUIDs())
}

func TestSyntheticManifestGroup_RenderRules(t *testing.T) {
	start := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	stop := start.Add(10 * time.Second)

	cases := []struct {
		name  string
		class enrichment.SyntheticClassification
		want  string
	}{
		{
			name:  "empty falls back to bare build",
			class: enrichment.SyntheticClassification{},
			want:  "build",
		},
		{
			name:  "command only",
			class: enrichment.SyntheticClassification{Command: "prepareForIndexing"},
			want:  "prepareForIndexing",
		},
		{
			name:  "action only (no command)",
			class: enrichment.SyntheticClassification{Action: "indexbuild"},
			want:  "indexbuild",
		},
		{
			name:  "command wins over action",
			class: enrichment.SyntheticClassification{Command: "prepareForIndexing", Action: "indexbuild"},
			want:  "prepareForIndexing",
		},
		{
			name:  "command with configuration",
			class: enrichment.SyntheticClassification{Command: "build", Configuration: "Debug"},
			want:  "build Debug",
		},
		{
			name:  "command with configuration and container",
			class: enrichment.SyntheticClassification{Command: "prepareForIndexing", Configuration: "Debug", Container: "IceCubesApp"},
			want:  "prepareForIndexing Debug [IceCubesApp]",
		},
		{
			name:  "action with container (no configuration)",
			class: enrichment.SyntheticClassification{Action: "clean", Container: "IceCubesApp"},
			want:  "clean [IceCubesApp]",
		},
		{
			name:  "unknown action passes through verbatim",
			class: enrichment.SyntheticClassification{Action: "customAction", Container: "MyApp"},
			want:  "customAction [MyApp]",
		},
		{
			name:  "container only renders bare build container",
			class: enrichment.SyntheticClassification{Container: "MyApp"},
			want:  "build [MyApp]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := enrichment.SyntheticManifestGroup("sidecar-x", start, stop, "", "", "", tc.class)
			assert.Equal(t, tc.want, g.Command())
		})
	}
}

// Classification on a synthetic group must not affect manifest-sourced groups —
// those still go through the Primary() path.
func TestSyntheticClassification_DoesNotLeakIntoManifestSourcedGroups(t *testing.T) {
	base := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	g := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{{
		UUID:       "u1",
		SchemeName: "S",
		Signature:  "Build S",
		Status:     "S",
		Start:      base,
		Stop:       base.Add(10 * time.Second),
	}}}

	// Manifest-derived groups never carry a classification, so the render stays on the Primary() path.
	assert.Equal(t, "build [S]", g.Command())
}

// v6 reproducer: Clean then Build on the same scheme ~1 s apart — two user
// actions, two BE rows. Hex is irrelevant when Command differs.
func TestGroupManifestEntries_CleanThenBuildOneSecondApart_Splits(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	entries := []enrichment.ManifestEntry{
		{UUID: "clean", SchemeName: "S", Signature: "Cleaning project Foo with scheme S", Status: "S", Start: base, Stop: base.Add(500 * time.Millisecond)},
		{UUID: "build", SchemeName: "S", Signature: "Build S", Status: "S", Start: base.Add(1500 * time.Millisecond), Stop: base.Add(10 * time.Second)},
	}

	lookup := hashLookup(map[string]string{"clean": "abcd1234", "build": "abcd1234"})

	groups := enrichment.GroupManifestEntries(entries, enrichment.WithMetricsLookup(lookup))
	require.Len(t, groups, 2)
	assert.Equal(t, []string{"clean"}, groups[0].UUIDs())
	assert.Equal(t, []string{"build"}, groups[1].UUIDs())
}
