//go:build unit

package xcodebuildinfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseXcodebuildList_workspaceShape(t *testing.T) {
	raw := []byte(`{
		"workspace": {
			"name": "App",
			"schemes": ["App", "AppTests"]
		}
	}`)

	info, err := parseXcodebuildList(raw)
	require.NoError(t, err)
	require.NotNil(t, info.Workspace)
	assert.Nil(t, info.Project)
	assert.Equal(t, "App", info.Workspace.Name)
	assert.Equal(t, []string{"App", "AppTests"}, info.Workspace.Schemes)
}

func Test_parseXcodebuildList_projectShape(t *testing.T) {
	raw := []byte(`{
		"project": {
			"configurations": ["Debug", "Release"],
			"name": "App",
			"schemes": ["App"],
			"targets": ["App", "AppTests"]
		}
	}`)

	info, err := parseXcodebuildList(raw)
	require.NoError(t, err)
	require.NotNil(t, info.Project)
	assert.Nil(t, info.Workspace)
	assert.Equal(t, "App", info.Project.Name)
	assert.Equal(t, []string{"App"}, info.Project.Schemes)
	assert.Equal(t, []string{"Debug", "Release"}, info.Project.Configurations)
	assert.Equal(t, []string{"App", "AppTests"}, info.Project.Targets)
}

func Test_parseXcodebuildList_malformedJSON_returnsError(t *testing.T) {
	_, err := parseXcodebuildList([]byte("not json"))
	require.Error(t, err)
}

func canonicals(dests []Destination) []string {
	out := make([]string, len(dests))
	for i, d := range dests {
		out[i] = d.Canonical
	}

	return out
}

func Test_parseShowDestinations(t *testing.T) {
	output := `
Available destinations for the "App" scheme:
    { platform:iOS Simulator, id:ABC-123, OS:17.4, name:iPhone 15 }
    { platform:iOS Simulator, id:DEF-456, OS:17.4, name:iPhone 15 Pro }
    { platform:macOS, arch:arm64, id:mac-1, name:My Mac }
    { platform:iOS, id:any-ios, name:Any iOS Device }
    { platform:iOS Simulator, id:generic-ios-sim, name:Any iOS Simulator Device }
`

	got := parseShowDestinations(output)
	// Ranking puts the newest iPhone first (Pro beats plain), then the other
	// iOS Simulator rows (iPhone 15, generic sim), then non-simulators in
	// Xcode's declared order.
	assert.Equal(t, []string{
		"platform=iOS Simulator,name=iPhone 15 Pro",
		"platform=iOS Simulator,name=iPhone 15",
		"generic/platform=iOS Simulator",
		"platform=macOS,name=My Mac",
		"generic/platform=iOS",
	}, canonicals(got))
}

func Test_parseShowDestinations_modernXcodeHeader(t *testing.T) {
	output := `
Command line invocation:
    /Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild -showdestinations -scheme Account

	Destinations compatible with the "Account" scheme:
		{ platform:iOS Simulator, arch:arm64, id:AAA, OS:27.0, name:iPhone 17 Pro }
		{ platform:iOS Simulator, arch:arm64, id:BBB, OS:27.0, name:iPhone 17 }

	Ineligible destinations for the "Account" scheme:
		{ platform:macOS, variant:Mac Catalyst, error:... }
`

	got := parseShowDestinations(output)
	assert.Equal(t, []string{
		"platform=iOS Simulator,name=iPhone 17 Pro",
		"platform=iOS Simulator,name=iPhone 17",
	}, canonicals(got))
}

func Test_parseShowDestinations_dedupesIdenticalCanonicalForms(t *testing.T) {
	// Two entries that differ only by fields not carried into the canonical
	// string (id / OS / arch) collapse to a single destination.
	output := `
Available destinations for the "App" scheme:
    { platform:iOS Simulator, id:aaa, OS:17.4, name:iPhone 15 }
    { platform:iOS Simulator, id:bbb, OS:18.0, name:iPhone 15 }
`

	got := parseShowDestinations(output)
	require.Len(t, got, 1)
	assert.Equal(t, "platform=iOS Simulator,name=iPhone 15", got[0].Canonical)
}

func Test_parseShowDestinations_skipsNoisyLines(t *testing.T) {
	output := `
Available destinations for the "App" scheme:

    { platform:iOS Simulator, id:x, name:iPhone 15 }

Ineligible destinations for the "App" scheme:
`

	got := parseShowDestinations(output)
	assert.Equal(t, []string{"platform=iOS Simulator,name=iPhone 15"}, canonicals(got))
}

func Test_parseShowDestinations_missingPlatformDropped(t *testing.T) {
	// If a line has no platform key we can't build a canonical -destination string.
	got := parseShowDestinations("    { id:x, name:whatever }\n")
	assert.Empty(t, got)
}

func Test_canonicalDestination_fallsBackToGenericPlatform_whenNameMissing(t *testing.T) {
	got := canonicalDestination(map[string]string{"platform": "macOS"})
	assert.Equal(t, "generic/platform=macOS", got)
}

func Test_parseShowDestinations_leadingWarningLines(t *testing.T) {
	output := `2026-08-13 10:22:11.123 xcodebuild[12345:67890] warning: could not resolve X
Some other unrelated line

Available destinations for the "App" scheme:
    { platform:iOS Simulator, id:x, name:iPhone 15 }
    { platform:macOS, arch:arm64, id:mac-1, name:My Mac }
`

	got := parseShowDestinations(output)
	assert.Equal(t, []string{
		"platform=iOS Simulator,name=iPhone 15",
		"platform=macOS,name=My Mac",
	}, canonicals(got))
}

func Test_parseShowDestinations_excludesIneligible(t *testing.T) {
	output := `
Available destinations for the "App" scheme:
    { platform:iOS Simulator, id:aaa, name:iPhone 15 }
    { platform:macOS, id:mac-1, name:My Mac }

Ineligible destinations for the "App" scheme:
    { platform:iOS Simulator, id:ineligible-1, name:iPhone SE (uninstalled), error:Runtime not installed }
    { platform:tvOS Simulator, id:ineligible-2, name:Apple TV, error:Runtime not installed }
`

	got := parseShowDestinations(output)
	assert.Equal(t, []string{
		"platform=iOS Simulator,name=iPhone 15",
		"platform=macOS,name=My Mac",
	}, canonicals(got))
}

func Test_parseXcodebuildList_workspaceZeroSchemes(t *testing.T) {
	raw := []byte(`{"workspace":{"name":"App","schemes":[]}}`)

	info, err := parseXcodebuildList(raw)
	require.NoError(t, err)
	require.NotNil(t, info.Workspace)
	assert.Empty(t, info.Workspace.Schemes)
	assert.Nil(t, info.Project)
}

func Test_DefaultDestination_latestIOSOnNewestIPhone(t *testing.T) {
	output := `
Available destinations for the "App" scheme:
    { platform:iOS Simulator, id:a, OS:17.0, name:iPhone 14 }
    { platform:iOS Simulator, id:b, OS:17.4, name:iPhone 15 }
    { platform:iOS Simulator, id:c, OS:18.1, name:iPhone 15 Pro }
    { platform:iOS Simulator, id:d, OS:18.1, name:iPhone 16 }
`

	dests := parseShowDestinations(output)
	def, ok := DefaultDestination(dests)
	require.True(t, ok)
	assert.Equal(t, "platform=iOS Simulator,name=iPhone 16", def.Canonical)
}

func Test_DefaultDestination_prefersSimulatorOverPhysical(t *testing.T) {
	output := `
Available destinations for the "App" scheme:
    { platform:iOS, id:any-ios, name:Any iOS Device }
    { platform:macOS, arch:arm64, id:mac-1, name:My Mac }
    { platform:iOS Simulator, id:a, OS:17.4, name:iPhone 15 }
`

	dests := parseShowDestinations(output)
	def, ok := DefaultDestination(dests)
	require.True(t, ok)
	assert.Equal(t, "iOS Simulator", def.Platform)
	assert.Equal(t, "platform=iOS Simulator,name=iPhone 15", def.Canonical)
}

func Test_DefaultDestination_fallsBackToFirstWhenNoSimulators(t *testing.T) {
	output := `
Available destinations for the "App" scheme:
    { platform:macOS, arch:arm64, id:mac-1, name:My Mac }
    { platform:iOS, id:any-ios, name:Any iOS Device }
`

	dests := parseShowDestinations(output)
	def, ok := DefaultDestination(dests)
	require.True(t, ok)
	// With no iOS Simulator rows, ranking preserves Xcode's declared order:
	// macOS came first in the output.
	assert.Equal(t, "platform=macOS,name=My Mac", def.Canonical)
}

func Test_DefaultDestination_tiebreakerIPhoneSuffixTiers(t *testing.T) {
	output := `
Available destinations for the "App" scheme:
    { platform:iOS Simulator, id:a, OS:17.4, name:iPhone 15 }
    { platform:iOS Simulator, id:b, OS:17.4, name:iPhone 15 Pro Max }
    { platform:iOS Simulator, id:c, OS:17.4, name:iPhone 15 Pro }
    { platform:iOS Simulator, id:d, OS:17.4, name:iPhone 15 Plus }
`

	dests := parseShowDestinations(output)
	def, ok := DefaultDestination(dests)
	require.True(t, ok)
	assert.Equal(t, "platform=iOS Simulator,name=iPhone 15 Pro Max", def.Canonical)
}

func Test_parseShowDestinations_dedupKeepsHighestOS(t *testing.T) {
	output := `
Available destinations for the "App" scheme:
    { platform:iOS Simulator, id:a, OS:17.0, name:iPhone 15 }
    { platform:iOS Simulator, id:b, OS:18.1, name:iPhone 15 }
    { platform:iOS Simulator, id:c, OS:17.4, name:iPhone 15 }
`

	got := parseShowDestinations(output)
	require.Len(t, got, 1)
	assert.Equal(t, "platform=iOS Simulator,name=iPhone 15", got[0].Canonical)
	assert.Equal(t, "18.1", got[0].OS, "dedup must keep the ranked (highest-OS) row")
}
