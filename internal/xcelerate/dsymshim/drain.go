package dsymshim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// DrainSummary aggregates one xcodebuild run's worth of per-invocation touch files.
type DrainSummary struct {
	// InvocationCount is the number of successful shim runs that rewrote argv.
	InvocationCount int
	// BypassCount is the number of shim runs that fell through to stock dsymutil
	// (killswitch, feature-unsupported, resolve-failure, idempotent passthrough).
	BypassCount int
	// Resolved sums the "resolved CAS ids" counter across all shim invocations.
	Resolved int
	// Missed sums "no such file or directory" warnings the plugin couldn't resolve.
	Missed int
	// FilteredStderrLines sums the paired warning+note stderr lines the filter stripped.
	FilteredStderrLines int
	// TotalDurationMs sums the per-invocation durations.
	TotalDurationMs int64
}

// Drain reads every NDJSON touch file under the drop dir newer than since, aggregates
// counts, and best-effort removes the consumed files. Files that can't be parsed are
// skipped silently; a corrupt touch file never fails the wrapper. Returns the aggregate
// and (separately) whether the shim is installed on disk, so analytics can distinguish
// "shim never shipped" from "shim shipped, nothing to do this run".
func Drain(p paths.Paths, since time.Time) DrainSummary {
	var out DrainSummary

	dir := p.XcelerateDsymShimDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".ndjson") {
			continue
		}

		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !since.IsZero() && info.ModTime().Before(since) {
			continue
		}

		data, err := os.ReadFile(path) //nolint:gosec // path controlled by shim
		if err != nil {
			continue
		}

		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			var rec touchRecord
			if json.Unmarshal([]byte(line), &rec) != nil {
				continue
			}

			if strings.HasPrefix(rec.Mode, "bypass:") {
				out.BypassCount++
			} else {
				out.InvocationCount++
			}

			out.Resolved += rec.ResolvedCount
			out.Missed += rec.MissedCount
			out.FilteredStderrLines += rec.FilteredStderr
			out.TotalDurationMs += rec.DurationMillis
		}

		_ = os.Remove(path)
	}

	return out
}

// ShimInstalled returns true when the on-disk shim binary exists. Separates
// "feature deployed" from "feature exercised" in analytics.
func ShimInstalled(p paths.Paths) bool {
	_, err := os.Stat(p.DsymutilCasShimPath())

	return err == nil
}

// StampAgeHours returns the age in hours since the toolchain farm's stamp.json was
// last written. Returns -1 when the stamp file is missing or unreadable; useful as a
// cheap drift signal in analytics without a separate audit path.
func StampAgeHours(p paths.Paths, now time.Time) int {
	stampPath := p.DsymutilCasShimToolchainStampFile()
	info, err := os.Stat(stampPath)
	if err != nil {
		return -1
	}

	age := now.Sub(info.ModTime())

	return int(age / time.Hour) //nolint:gosec // bounded age fits int
}
