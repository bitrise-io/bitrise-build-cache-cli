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
		"Build MyScheme":          enrichment.CommandBuild,
		"Building MyScheme":       enrichment.CommandBuild,
		"Cleaning MyScheme":       enrichment.CommandBuild,
		"Test MySchemeTests":      enrichment.CommandTest,
		"Testing MySchemeTests":   enrichment.CommandTest,
		"Archive MyScheme":        enrichment.CommandArchive,
		"Archiving MyScheme":      enrichment.CommandArchive,
		"Analyzing MyScheme":      enrichment.CommandUnknown,
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
