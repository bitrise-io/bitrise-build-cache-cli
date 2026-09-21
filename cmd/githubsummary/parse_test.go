//go:build unit

package githubsummary

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Verbatim from a Build Hub e2e run, GitHub's timestamps included, so the
// patterns are exercised against what the plugins really print.
const realLog = `
2026-09-21T08:34:59.8444173Z [Bitrise Build Cache] [08:34:59] 🤖 Bitrise remote cache enabled (id: 8836)
2026-09-21T08:34:59.8453247Z [Bitrise Build Cache] [08:34:59] Endpoint: grpcs://bitrise-accelerate.services.bitrise.io
2026-09-21T08:34:59.8533419Z [Bitrise Build Cache] [08:34:59] invocationId - 0ffffe79-41ca-3a74-b3d9-a2a5dbec04bf
2026-09-21T08:34:59.8542993Z [Bitrise Build Cache] [08:34:59] Remote cache plugin version: 2.2.0
2026-09-21T08:35:20.0422773Z [Bitrise Analytics] [08:35:19] Gradle task stats (androidRoot): task hits: 1583 (from cache: 1583, up to date: 0) / actionable tasks: 2233 of 2235 Gradle counts (70.89%)
2026-09-21T08:35:21.0409750Z [Bitrise Analytics] [08:35:20] 2428 tasks uploaded. Check invocation at https://app.bitrise.io/build-cache/invocations/gradle/0ffffe79-41ca-3a74-b3d9-a2a5dbec04bf
2026-09-21T08:35:21.1437448Z 2235 actionable tasks: 652 executed, 1583 from cache
2026-09-21T08:35:21.3410656Z [Bitrise Build Cache] [08:35:21] Bitrise remote build cache stats (8836): blob hits: 2595 (79.2 MB) / blob lookups: 2595 (100.00%) over 1967 distinct blobs. Uploaded: 0 (0 B)
`

func TestParse_realBuildLog(t *testing.T) {
	s := Parse(strings.NewReader(realLog))

	require.False(t, s.Empty())

	assert.Equal(t, 1583, s.TasksFromCache)
	assert.Equal(t, 652, s.TasksExecuted)
	assert.Equal(t, 2235, s.TasksTotal)
	// 1583/2235 from Gradle's totals. The plugin prints 70.89% because it
	// divides by its own count of 2233 -- close, but a different denominator,
	// and Gradle's is the one that covers the whole invocation.
	assert.InDelta(t, 70.83, s.TaskHitRate, 0.01)

	assert.Equal(t, 2595, s.BlobHits)
	assert.Equal(t, 2595, s.BlobLookups)
	assert.InDelta(t, 100.0, s.BlobHitRate, 0.001)
	assert.Equal(t, "79.2 MB", FormatSize(s.DownloadedBytes))
	assert.Equal(t, "0 B", FormatSize(s.UploadedBytes))

	assert.Equal(t, "0ffffe79-41ca-3a74-b3d9-a2a5dbec04bf", s.InvocationID)
	assert.Equal(t, "grpcs://bitrise-accelerate.services.bitrise.io", s.CacheEndpoint)
	assert.Equal(t, "2.2.0", s.PluginVersion)
}

// A cold cache uploads instead of downloading, and nothing comes from cache.
func TestParse_coldCache(t *testing.T) {
	const log = `
[Bitrise Analytics] Gradle task stats (androidRoot): task hits: 0 (from cache: 0, up to date: 0) / actionable tasks: 2233 of 2235 Gradle counts (0.00%)
2235 actionable tasks: 2235 executed, 0 from cache
[Bitrise Build Cache] Bitrise remote build cache stats (8836): blob hits: 0 (0 B) / blob lookups: 1967 (0.00%) over 1967 distinct blobs. Uploaded: 1842 (312.4 MB)
`
	s := Parse(strings.NewReader(log))

	assert.Equal(t, 0, s.TasksFromCache)
	assert.Equal(t, 2235, s.TasksExecuted)
	assert.InDelta(t, 0.0, s.BlobHitRate, 0.001)
	assert.Equal(t, "312.4 MB", FormatSize(s.UploadedBytes))
}

// Without Gradle's totals line the plugin's per-build lines are all there is,
// so they add up: buildSrc's work is real work the cache served.
func TestParse_sumsPluginTaskStatsWhenNoGradleTotals(t *testing.T) {
	const log = `
[Bitrise Analytics] Gradle task stats (buildSrc): task hits: 7 (from cache: 7, up to date: 0) / actionable tasks: 10 of 10 Gradle counts (70.00%)
[Bitrise Analytics] Gradle task stats (androidRoot): task hits: 1583 (from cache: 1583, up to date: 0) / actionable tasks: 2233 of 2235 Gradle counts (70.89%)
`
	s := Parse(strings.NewReader(log))

	assert.Equal(t, 1590, s.TasksFromCache)
	assert.Equal(t, 2245, s.TasksTotal)
}

// With Gradle's own totals present, they win: they describe the invocation
// rather than one of its builds.
func TestParse_gradleTotalsOverridePluginLines(t *testing.T) {
	const log = `
[Bitrise Analytics] Gradle task stats (buildSrc): task hits: 7 (from cache: 7, up to date: 0) / actionable tasks: 10 of 10 Gradle counts (70.00%)
2235 actionable tasks: 652 executed, 1583 from cache
`
	s := Parse(strings.NewReader(log))

	assert.Equal(t, 1583, s.TasksFromCache)
	assert.Equal(t, 2235, s.TasksTotal)
	assert.Equal(t, 652, s.TasksExecuted)
}

func TestParse_noPluginOutput(t *testing.T) {
	s := Parse(strings.NewReader("> Task :app:compileDebugKotlin\nBUILD SUCCESSFUL in 3s\n"))

	assert.True(t, s.Empty())
}

func TestParse_upToDateTasksAreKept(t *testing.T) {
	const log = `[Bitrise Analytics] Gradle task stats (androidRoot): task hits: 120 (from cache: 100, up to date: 20) / actionable tasks: 200 of 200 Gradle counts (60.00%)`

	s := Parse(strings.NewReader(log))

	assert.Equal(t, 100, s.TasksFromCache)
	assert.Equal(t, 20, s.TasksUpToDate)
}

// A build prints one stats line per included build. buildSrc's line came first
// in a real run, so anything that takes the first or the last one reports a
// fraction of the transfer.
func TestParse_sumsStatsAcrossIncludedBuilds(t *testing.T) {
	const log = `
[Bitrise Build Cache] Bitrise remote build cache stats (8901): blob hits: 24 (2.5 MB) / blob lookups: 24 (100.00%) over 24 distinct blobs. Uploaded: 0 (0 B)
[Bitrise Build Cache] Bitrise remote build cache stats (8836): blob hits: 2595 (79.2 MB) / blob lookups: 2595 (100.00%) over 1967 distinct blobs. Uploaded: 3 (1.5 MB)
`
	s := Parse(strings.NewReader(log))

	assert.Equal(t, 2619, s.BlobHits)
	assert.Equal(t, 2619, s.BlobLookups)
	assert.Equal(t, "81.7 MB", FormatSize(s.DownloadedBytes))
	assert.Equal(t, "1.5 MB", FormatSize(s.UploadedBytes))
	assert.InDelta(t, 100.0, s.BlobHitRate, 0.01)
}

func TestParse_sizesRoundTrip(t *testing.T) {
	for _, in := range []string{"0 B", "512 B", "2.5 MB", "79.2 MB", "1.2 GB"} {
		assert.Equal(t, in, FormatSize(parseSize(in)), "round trip %s", in)
	}
}

// The hit rate is computed, not read off one plugin line, so it describes the
// whole invocation rather than whichever build printed last.
func TestParse_hitRateFromTotals(t *testing.T) {
	const log = `2235 actionable tasks: 652 executed, 1583 from cache`

	s := Parse(strings.NewReader(log))

	assert.InDelta(t, 70.83, s.TaskHitRate, 0.01)
}
