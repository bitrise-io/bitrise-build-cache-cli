package githubsummary

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Summary is what the Gradle plugins report at the end of a build.
type Summary struct {
	InvocationID  string
	InvocationURL string
	CacheEndpoint string
	PluginVersion string

	TasksTotal     int
	TasksFromCache int
	TasksUpToDate  int
	TasksExecuted  int
	TaskHitRate    float64
	HasTaskStats   bool

	pluginFromCache int
	pluginUpToDate  int
	pluginTotal     int
	gradleFromCache int
	gradleExecuted  int
	gradleTotal     int

	BlobHits        int
	BlobLookups     int
	BlobHitRate     float64
	DownloadedBytes int64
	UploadedBytes   int64
	HasCacheStats   bool
}

// The plugins prefix every line and Actions prefixes a timestamp, so each
// pattern is anchored on wording rather than position.
var (
	// Gradle task stats (androidRoot): task hits: 1583 (from cache: 1583, up to date: 0) / actionable tasks: 2233 of 2235 Gradle counts (70.89%)
	reTaskStats = regexp.MustCompile(
		`task hits: (\d+) \(from cache: (\d+), up to date: (\d+)\) / actionable tasks: (\d+) of (\d+) Gradle counts \(([\d.]+)%\)`)
	// 2235 actionable tasks: 652 executed, 1583 from cache
	reGradleTotals = regexp.MustCompile(`(\d+) actionable tasks: (\d+) executed, (\d+) from cache`)
	// blob hits: 2595 (79.2 MB) / blob lookups: 2595 (100.00%) over 1967 distinct blobs. Uploaded: 0 (0 B)
	reCacheStats = regexp.MustCompile(
		`blob hits: (\d+) \(([^)]+)\) / blob lookups: (\d+) \(([\d.]+)%\).*?Uploaded: \d+ \(([^)]+)\)`)
	reInvocationURL = regexp.MustCompile(`Check invocation at (\S+/invocations/gradle/([0-9a-fA-F-]+))`)
	reEndpoint      = regexp.MustCompile(`Endpoint: (\S+)`)
	rePluginVersion = regexp.MustCompile(`Remote cache plugin version: (\S+)`)
)

// Parse reads a Gradle build log and picks out what the Bitrise plugins printed.
// Later lines win: a build with several included builds prints its stats more
// than once, and the last one covers the whole build.
func Parse(r io.Reader) Summary {
	var s Summary

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Also per build, so summed. Gradle's own totals line below overrides
		// these when present: it is authoritative for the whole invocation.
		if m := reTaskStats.FindStringSubmatch(line); m != nil {
			s.pluginFromCache += atoi(m[2])
			s.pluginUpToDate += atoi(m[3])
			s.pluginTotal += atoi(m[5])
			s.HasTaskStats = true
		}

		if m := reGradleTotals.FindStringSubmatch(line); m != nil {
			s.gradleTotal = atoi(m[1])
			s.gradleExecuted = atoi(m[2])
			s.gradleFromCache = atoi(m[3])
			s.HasTaskStats = true
		}

		// One line per build -- an included build such as buildSrc reports its
		// own -- so these add up rather than overwrite.
		if m := reCacheStats.FindStringSubmatch(line); m != nil {
			s.BlobHits += atoi(m[1])
			s.BlobLookups += atoi(m[3])
			s.DownloadedBytes += parseSize(m[2])
			s.UploadedBytes += parseSize(m[5])
			s.HasCacheStats = true
		}

		if m := reInvocationURL.FindStringSubmatch(line); m != nil {
			s.InvocationURL = m[1]
			s.InvocationID = m[2]
		}

		if m := reEndpoint.FindStringSubmatch(line); m != nil {
			s.CacheEndpoint = m[1]
		}

		if m := rePluginVersion.FindStringSubmatch(line); m != nil {
			s.PluginVersion = m[1]
		}
	}

	resolveTaskStats(&s)

	if s.BlobLookups > 0 {
		s.BlobHitRate = float64(s.BlobHits) / float64(s.BlobLookups) * 100
	}

	return s
}

// resolveTaskStats prefers Gradle's own end-of-build totals and falls back to
// the plugin's per-build lines. The hit rate is computed rather than read off a
// single line, which would only describe one of several builds.
func resolveTaskStats(s *Summary) {
	switch {
	case s.gradleTotal > 0:
		s.TasksTotal = s.gradleTotal
		s.TasksFromCache = s.gradleFromCache
		s.TasksExecuted = s.gradleExecuted
		s.TasksUpToDate = s.pluginUpToDate
	case s.pluginTotal > 0:
		s.TasksTotal = s.pluginTotal
		s.TasksFromCache = s.pluginFromCache
		s.TasksUpToDate = s.pluginUpToDate
		s.TasksExecuted = s.pluginTotal - s.pluginFromCache - s.pluginUpToDate
	}

	if s.TasksTotal > 0 {
		s.TaskHitRate = float64(s.TasksFromCache+s.TasksUpToDate) / float64(s.TasksTotal) * 100
	}
}

// Empty reports whether the log carried no Bitrise plugin output at all, which
// is the case worth telling the user about rather than rendering blank tables.
func (s Summary) Empty() bool {
	return !s.HasTaskStats && !s.HasCacheStats
}

func atoi(s string) int {
	v, _ := strconv.Atoi(s)

	return v
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)

	return v
}

var sizeUnits = map[string]float64{ //nolint:gochecknoglobals
	"B": 1, "KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30, "TB": 1 << 40,
}

var reSize = regexp.MustCompile(`^([\d.]+)\s*([KMGT]?B)$`)

// parseSize reads the human-readable sizes the plugin prints ("79.2 MB", "0 B")
// so several builds' figures can be added together.
func parseSize(s string) int64 {
	m := reSize.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}

	mult, ok := sizeUnits[m[2]]
	if !ok {
		return 0
	}

	return int64(atof(m[1]) * mult)
}

// FormatSize renders a byte count the way the plugin would.
func FormatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return strconv.FormatFloat(float64(b)/(1<<30), 'f', 1, 64) + " GB"
	case b >= 1<<20:
		return strconv.FormatFloat(float64(b)/(1<<20), 'f', 1, 64) + " MB"
	case b >= 1<<10:
		return strconv.FormatFloat(float64(b)/(1<<10), 'f', 1, 64) + " KB"
	default:
		return strconv.FormatInt(b, 10) + " B"
	}
}
