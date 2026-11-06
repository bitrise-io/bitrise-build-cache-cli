//go:build unit

package enrichment_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

const buildPlistHeader = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logFormatVersion</key>
	<integer>11</integer>
	<key>logs</key>
	<dict>
`

const buildPlistFooter = `	</dict>
</dict>
</plist>
`

func writeBoundaryPlist(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "LogStoreManifest.plist")
	require.NoError(t, os.WriteFile(path, []byte(buildPlistHeader+body+buildPlistFooter), 0o644))

	return path
}

func TestReadBoundaries_MissingFileReturnsOSError(t *testing.T) {
	_, err := enrichment.ReadBoundaries(filepath.Join(t.TempDir(), "nope.plist"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist), "ENOENT must propagate (via error wrapping) so callers can distinguish 'no DD / no builds yet' from a decode failure")
}

func TestReadBoundaries_EmptyPlistReturnsNoBoundaries(t *testing.T) {
	path := writeBoundaryPlist(t, ``)

	got, err := enrichment.ReadBoundaries(path)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestReadBoundaries_ParsesIDEMaster(t *testing.T) {
	// 762345678.5 CFAbsoluteTime == 2025-02-15 10:41:18.5 UTC
	path := writeBoundaryPlist(t, `
		<key>AAAA-IDE-BUILD</key>
		<dict>
			<key>className</key><string>IDEActivityLogSection</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.BuildLog</string>
			<key>fileName</key><string>AAAA-IDE-BUILD.xcactivitylog</string>
			<key>highLevelStatus</key><string>S</string>
			<key>schemeIdentifier-schemeName</key><string>IceCubesApp</string>
			<key>signature</key><string>Build IceCubesApp</string>
			<key>timeStartedRecording</key><real>762345678.5</real>
			<key>timeStoppedRecording</key><real>762345688.75</real>
			<key>title</key><string>Build IceCubesApp</string>
			<key>uniqueIdentifier</key><string>AAAA-IDE-BUILD</string>
		</dict>
`)

	got, err := enrichment.ReadBoundaries(path)
	require.NoError(t, err)
	require.Len(t, got, 1)

	assert.Equal(t, "AAAA-IDE-BUILD", got[0].UniqueIdentifier)
	assert.Equal(t, enrichment.DomainBuild, got[0].Domain)
	assert.Equal(t, "Build IceCubesApp", got[0].Signature)
	assert.Equal(t, "IceCubesApp", got[0].SchemeName)
	assert.Equal(t, "S", got[0].Status)
	assert.False(t, got[0].Start.IsZero())
	assert.True(t, got[0].Stop.After(got[0].Start))
}

// TestReadBoundaries_MultiDomainOrder (the renamed/rescoped successor of
// TestReadBuildBoundaries_FiltersPackageSigningLocalization) pins S8's
// semantics: all four attribution-eligible domains surface — Build, Package,
// Signing, Localization — sorted by Start ascending, each tagged with its
// Domain so the bridge can label the BE row with the right short command.
func TestReadBoundaries_MultiDomainOrder(t *testing.T) {
	path := writeBoundaryPlist(t, `
		<key>AAAA-BUILD</key>
		<dict>
			<key>className</key><string>IDEActivityLogSection</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.BuildLog</string>
			<key>timeStartedRecording</key><real>762345678.5</real>
			<key>timeStoppedRecording</key><real>762345688.75</real>
			<key>signature</key><string>Build Scheme</string>
		</dict>
		<key>PACKAGE-LOAD</key>
		<dict>
			<key>className</key><string>IDEActivityLogPackageLoadLog</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.PackageLog</string>
			<key>timeStartedRecording</key><real>762345670.0</real>
			<key>timeStoppedRecording</key><real>762345672.0</real>
			<key>signature</key><string>Resolve Packages</string>
		</dict>
		<key>SIGNING</key>
		<dict>
			<key>className</key><string>IDEActivityLogProvisioningSection</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.ProvisioningLog</string>
			<key>timeStartedRecording</key><real>762345671.0</real>
			<key>timeStoppedRecording</key><real>762345673.0</real>
			<key>signature</key><string>Update Signing</string>
		</dict>
		<key>LOCALIZATION</key>
		<dict>
			<key>className</key><string>IDEActivityLogLocalizationSection</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.LocalizationLog</string>
			<key>timeStartedRecording</key><real>762345674.0</real>
			<key>timeStoppedRecording</key><real>762345675.0</real>
			<key>signature</key><string>Sync Localizations</string>
		</dict>
`)

	got, err := enrichment.ReadBoundaries(path)
	require.NoError(t, err)
	require.Len(t, got, 4, "all four attribution-eligible domains must surface")

	assert.Equal(t, "PACKAGE-LOAD", got[0].UniqueIdentifier, "Start-ascending order places Package first")
	assert.Equal(t, enrichment.DomainPackage, got[0].Domain)
	assert.Equal(t, "Resolve Packages", got[0].Signature)

	assert.Equal(t, "SIGNING", got[1].UniqueIdentifier)
	assert.Equal(t, enrichment.DomainSigning, got[1].Domain)
	assert.Equal(t, "Update Signing", got[1].Signature)

	assert.Equal(t, "LOCALIZATION", got[2].UniqueIdentifier)
	assert.Equal(t, enrichment.DomainLocalization, got[2].Domain)
	assert.Equal(t, "Sync Localizations", got[2].Signature)

	assert.Equal(t, "AAAA-BUILD", got[3].UniqueIdentifier)
	assert.Equal(t, enrichment.DomainBuild, got[3].Domain)
}

// TestReadBoundaries_FiltersUnknownDomains pins the inverse of the multi-
// domain acceptance: classNames / domainTypes outside the attribution set
// (plain xcactivitylog sections with no matching domainType) still drop so
// unknown entries don't leak into the bridge's enclosing lookup.
func TestReadBoundaries_FiltersUnknownDomains(t *testing.T) {
	path := writeBoundaryPlist(t, `
		<key>AAAA-BUILD</key>
		<dict>
			<key>className</key><string>IDEActivityLogSection</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.BuildLog</string>
			<key>timeStartedRecording</key><real>762345678.5</real>
			<key>timeStoppedRecording</key><real>762345688.75</real>
			<key>signature</key><string>Build Scheme</string>
		</dict>
		<key>UNKNOWN</key>
		<dict>
			<key>className</key><string>IDESomethingElse</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.MysteryLog</string>
			<key>timeStartedRecording</key><real>762345670.0</real>
			<key>timeStoppedRecording</key><real>762345672.0</real>
			<key>signature</key><string>Mystery</string>
		</dict>
`)

	got, err := enrichment.ReadBoundaries(path)
	require.NoError(t, err)
	require.Len(t, got, 1, "unrecognized domainType must not attribute — only Build/Package/Signing/Localization")
	assert.Equal(t, "AAAA-BUILD", got[0].UniqueIdentifier)
}

func TestReadBoundaries_LegacyClassNameFallbackWhenDomainTypeMissing(t *testing.T) {
	// Older Xcode versions omit domainType; the className allow-list covers
	// the four attribution-eligible families.
	path := writeBoundaryPlist(t, `
		<key>CLI-BUILD</key>
		<dict>
			<key>className</key><string>IDECommandLineBuildLog</string>
			<key>timeStartedRecording</key><real>762345678.5</real>
			<key>timeStoppedRecording</key><real>762345688.75</real>
			<key>signature</key><string>Build via xcodebuild CLI</string>
		</dict>
		<key>LEGACY-PACKAGE</key>
		<dict>
			<key>className</key><string>IDEActivityLogPackageLoadLog</string>
			<key>timeStartedRecording</key><real>762345670.0</real>
			<key>timeStoppedRecording</key><real>762345672.0</real>
			<key>signature</key><string>Resolve Packages</string>
		</dict>
		<key>LEGACY-SIGNING</key>
		<dict>
			<key>className</key><string>IDEActivityLogProvisioningSection</string>
			<key>timeStartedRecording</key><real>762345671.0</real>
			<key>timeStoppedRecording</key><real>762345673.0</real>
			<key>signature</key><string>Update Signing</string>
		</dict>
`)

	got, err := enrichment.ReadBoundaries(path)
	require.NoError(t, err)
	require.Len(t, got, 3)

	byUUID := map[string]enrichment.Boundary{}
	for _, b := range got {
		byUUID[b.UniqueIdentifier] = b
	}
	assert.Equal(t, enrichment.DomainBuild, byUUID["CLI-BUILD"].Domain)
	assert.Equal(t, enrichment.DomainPackage, byUUID["LEGACY-PACKAGE"].Domain)
	assert.Equal(t, enrichment.DomainSigning, byUUID["LEGACY-SIGNING"].Domain)
}

func TestReadBoundaries_SortedByStartAscending(t *testing.T) {
	path := writeBoundaryPlist(t, `
		<key>SECOND</key>
		<dict>
			<key>className</key><string>IDEActivityLogSection</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.BuildLog</string>
			<key>timeStartedRecording</key><real>762345690.0</real>
			<key>timeStoppedRecording</key><real>762345695.0</real>
		</dict>
		<key>FIRST</key>
		<dict>
			<key>className</key><string>IDEActivityLogSection</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.BuildLog</string>
			<key>timeStartedRecording</key><real>762345678.0</real>
			<key>timeStoppedRecording</key><real>762345688.0</real>
		</dict>
		<key>THIRD</key>
		<dict>
			<key>className</key><string>IDEActivityLogSection</string>
			<key>domainType</key><string>Xcode.IDEActivityLogDomainType.BuildLog</string>
			<key>timeStartedRecording</key><real>762345700.0</real>
			<key>timeStoppedRecording</key><real>762345705.0</real>
		</dict>
`)

	got, err := enrichment.ReadBoundaries(path)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "FIRST", got[0].UniqueIdentifier)
	assert.Equal(t, "SECOND", got[1].UniqueIdentifier)
	assert.Equal(t, "THIRD", got[2].UniqueIdentifier)
}

func TestReadBoundaries_PrimaryObservableStatusFallback(t *testing.T) {
	// highLevelStatus moved under primaryObservable in newer Xcode versions for
	// CLI builds; the reader honors both locations.
	path := writeBoundaryPlist(t, `
		<key>CLI</key>
		<dict>
			<key>className</key><string>IDECommandLineBuildLog</string>
			<key>primaryObservable</key>
			<dict>
				<key>highLevelStatus</key><string>E</string>
			</dict>
			<key>timeStartedRecording</key><real>762345678.0</real>
			<key>timeStoppedRecording</key><real>762345688.0</real>
		</dict>
`)

	got, err := enrichment.ReadBoundaries(path)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "E", got[0].Status)
}

func TestEnclosingBoundary_SelectsEnclosing(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	boundaries := []enrichment.Boundary{
		{UniqueIdentifier: "A", Start: base, Stop: base.Add(10 * time.Second)},
		{UniqueIdentifier: "B", Start: base.Add(30 * time.Second), Stop: base.Add(40 * time.Second)},
	}

	// t inside A
	got, ok := enrichment.EnclosingBoundary(boundaries, base.Add(5*time.Second), enrichment.DefaultAttributionGrace)
	require.True(t, ok)
	assert.Equal(t, "A", got.UniqueIdentifier)

	// t inside B
	got, ok = enrichment.EnclosingBoundary(boundaries, base.Add(35*time.Second), enrichment.DefaultAttributionGrace)
	require.True(t, ok)
	assert.Equal(t, "B", got.UniqueIdentifier)

	// t between A and B — no enclosing
	_, ok = enrichment.EnclosingBoundary(boundaries, base.Add(20*time.Second), enrichment.DefaultAttributionGrace)
	assert.False(t, ok, "a time strictly between boundaries must not attribute")

	// t inside grace past A's Stop
	got, ok = enrichment.EnclosingBoundary(boundaries, base.Add(13*time.Second), enrichment.DefaultAttributionGrace)
	require.True(t, ok, "within grace, late-arriving dark build attributes to the preceding master")
	assert.Equal(t, "A", got.UniqueIdentifier)

	// t past grace
	_, ok = enrichment.EnclosingBoundary(boundaries, base.Add(16*time.Second), enrichment.DefaultAttributionGrace)
	assert.False(t, ok, "past grace, attribution must not latch onto a stale boundary")
}

func TestEnclosingBoundary_MostRecentPrecedingWins(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	// B overlaps A (cancelled+restart edge case): both windows enclose t, but
	// B's Start is later — "most recent preceding" is the attribution rule.
	boundaries := []enrichment.Boundary{
		{UniqueIdentifier: "A", Start: base, Stop: base.Add(30 * time.Second)},
		{UniqueIdentifier: "B", Start: base.Add(10 * time.Second), Stop: base.Add(20 * time.Second)},
	}

	got, ok := enrichment.EnclosingBoundary(boundaries, base.Add(15*time.Second), enrichment.DefaultAttributionGrace)
	require.True(t, ok)
	assert.Equal(t, "B", got.UniqueIdentifier, "overlapping windows: most-recent-preceding wins")
}

func TestEnclosingBoundary_ZeroAtReturnsFalse(t *testing.T) {
	boundaries := []enrichment.Boundary{
		{UniqueIdentifier: "A", Start: time.Now(), Stop: time.Now().Add(time.Second)},
	}
	_, ok := enrichment.EnclosingBoundary(boundaries, time.Time{}, enrichment.DefaultAttributionGrace)
	assert.False(t, ok)
}
