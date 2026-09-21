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

	TasksTotal    int
	TasksFromCache int
	TasksUpToDate int
	TasksExecuted int
	TaskHitRate   float64
	HasTaskStats  bool

	BlobHits     int
	BlobLookups  int
	BlobHitRate  float64
	Downloaded   string
	Uploaded     string
	HasCacheStats bool
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

		if m := reTaskStats.FindStringSubmatch(line); m != nil {
			s.TasksFromCache = atoi(m[2])
			s.TasksUpToDate = atoi(m[3])
			s.TasksTotal = atoi(m[5])
			s.TaskHitRate = atof(m[6])
			s.HasTaskStats = true
		}

		if m := reGradleTotals.FindStringSubmatch(line); m != nil {
			s.TasksTotal = atoi(m[1])
			s.TasksExecuted = atoi(m[2])
			s.TasksFromCache = atoi(m[3])
			s.HasTaskStats = true
		}

		if m := reCacheStats.FindStringSubmatch(line); m != nil {
			s.BlobHits = atoi(m[1])
			s.Downloaded = strings.TrimSpace(m[2])
			s.BlobLookups = atoi(m[3])
			s.BlobHitRate = atof(m[4])
			s.Uploaded = strings.TrimSpace(m[5])
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

	return s
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
