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
	assert.InDelta(t, 70.89, s.TaskHitRate, 0.001)

	assert.Equal(t, 2595, s.BlobHits)
	assert.Equal(t, 2595, s.BlobLookups)
	assert.InDelta(t, 100.0, s.BlobHitRate, 0.001)
	assert.Equal(t, "79.2 MB", s.Downloaded)
	assert.Equal(t, "0 B", s.Uploaded)

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
	assert.Equal(t, "312.4 MB", s.Uploaded)
}

// An included build prints its own stats first; the last line covers the whole
// build, so that is the one that has to win.
func TestParse_lastStatsLineWins(t *testing.T) {
	const log = `
[Bitrise Analytics] Gradle task stats (buildSrc): task hits: 7 (from cache: 7, up to date: 0) / actionable tasks: 10 of 10 Gradle counts (70.00%)
[Bitrise Analytics] Gradle task stats (androidRoot): task hits: 1583 (from cache: 1583, up to date: 0) / actionable tasks: 2233 of 2235 Gradle counts (70.89%)
`
	s := Parse(strings.NewReader(log))

	assert.Equal(t, 1583, s.TasksFromCache)
	assert.Equal(t, 2235, s.TasksTotal)
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
