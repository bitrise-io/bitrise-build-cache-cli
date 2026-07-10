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
	require.Len(t, entries, 2)

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
}

func TestLoadManifest_Missing(t *testing.T) {
	_, err := enrichment.LoadManifest("testdata/does-not-exist.plist")
	require.Error(t, err)
}
