//go:build unit

package interactive

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
	assert.Equal(t, []string{
		"generic/platform=iOS",
		"generic/platform=iOS Simulator",
		"platform=iOS Simulator,name=iPhone 15",
		"platform=iOS Simulator,name=iPhone 15 Pro",
		"platform=macOS,name=My Mac",
	}, got)
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
	assert.Equal(t, []string{"platform=iOS Simulator,name=iPhone 15"}, got)
}

func Test_parseShowDestinations_skipsNoisyLines(t *testing.T) {
	output := `
Available destinations for the "App" scheme:

    { platform:iOS Simulator, id:x, name:iPhone 15 }

Ineligible destinations for the "App" scheme:
`

	got := parseShowDestinations(output)
	assert.Equal(t, []string{"platform=iOS Simulator,name=iPhone 15"}, got)
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
	}, got)
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
	}, got)
}

func Test_parseXcodebuildList_workspaceZeroSchemes(t *testing.T) {
	raw := []byte(`{"workspace":{"name":"App","schemes":[]}}`)

	info, err := parseXcodebuildList(raw)
	require.NoError(t, err)
	require.NotNil(t, info.Workspace)
	assert.Empty(t, info.Workspace.Schemes)
	assert.Nil(t, info.Project)
}
