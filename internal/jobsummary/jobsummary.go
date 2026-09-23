// Package jobsummary renders a tool's end-of-build cache stats onto the GitHub
// Actions job page, from the counters the CLI already holds.
//
// GITHUB_STEP_SUMMARY is a file the runner creates for the step, and the CLI
// inherits it like any other environment variable, so this needs nothing added
// to the user's workflow.
//
// One table for the job, one row per invocation, appended as each finishes. A
// job that builds and then tests runs the cache very differently in each, and a
// single set of totals describes neither. The Gradle plugins write rows into the
// same table, so a mixed job reads as one list.
package jobsummary

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const (
	summaryEnvVar = "GITHUB_STEP_SUMMARY"

	startMarker = "<!-- bitrise-build-cache:table:start -->"
	endMarker   = "<!-- bitrise-build-cache:table:end -->"

	header = "## ⚡ Bitrise Build Cache\n\n" +
		"| Status | Command | Cache status | Downloaded | Uploaded | Duration | |\n" +
		"|:---:|---|---|---:|---:|---|---|\n"

	phaseBaseline = "baseline"
	phaseWarmup   = "warmup"

	bytesInMB = 1000 * 1000
)

// Invocation is one row: what ran, how the cache served it, and how long it took.
// Nil byte counts mean nothing reported, which is not the same as nothing moving.
type Invocation struct {
	Success         bool
	Command         string
	BenchmarkPhase  string
	DownloadedBytes *int64
	UploadedBytes   *int64
	Duration        time.Duration
	InvocationURL   string
}

func Row(i Invocation) string {
	status := "✅"
	if !i.Success {
		status = "❌"
	}

	command := i.Command
	if command == "" {
		command = "build"
	}

	link := ""
	if i.InvocationURL != "" {
		link = fmt.Sprintf("[View →](%s)", i.InvocationURL)
	}

	return fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s |\n",
		status, command, CacheStatus(i),
		megabytes(i.DownloadedBytes), megabytes(i.UploadedBytes),
		duration(i.Duration), link)
}

// CacheStatus applies the same ladder as the invocation list in the web UI, so a
// row here and a row there describe the same build the same way. See
// invocationCacheStatus.ts in bitrise-website.
func CacheStatus(i Invocation) string {
	if i.BenchmarkPhase == phaseBaseline {
		return "Baseline"
	}

	if i.DownloadedBytes == nil || i.UploadedBytes == nil {
		return "No activity"
	}

	down, up := *i.DownloadedBytes, *i.UploadedBytes
	if down == 0 && up == 0 {
		return "No activity"
	}

	if i.BenchmarkPhase == phaseWarmup {
		return "Warming up"
	}

	// Downloading as much as it uploaded still counts as reuse: the cache served
	// everything it was asked for, it just stored the same amount back.
	if down >= up {
		return "Healthy"
	}

	return "Low reuse"
}

// Write appends this invocation's row to the job's table, replacing the row when
// the same invocation reports twice. Reports whether anything was written; a
// summary is a nicety, so no caller should fail on it.
func Write(row, invocationURL string) (bool, error) {
	path := os.Getenv(summaryEnvVar)
	if path == "" || row == "" {
		return false, nil
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", summaryEnvVar, err)
	}

	if err := os.WriteFile(path, []byte(withRow(string(existing), row, invocationURL)), 0o644); err != nil { //nolint:gosec,mnd // the runner reads this file
		return false, fmt.Errorf("write %s: %w", summaryEnvVar, err)
	}

	return true, nil
}

func withRow(content, row, invocationURL string) string {
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)
	if start < 0 || end < start {
		return content + startMarker + "\n" + header + row + endMarker + "\n"
	}

	lines := strings.Split(content[start+len(startMarker):end], "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if invocationURL != "" && strings.Contains(line, invocationURL) {
			continue
		}
		kept = append(kept, line)
	}

	rows := strings.Trim(strings.Join(kept, "\n"), "\n")

	return content[:start] + startMarker + "\n" + rows + "\n" + row + content[end:]
}

// One decimal at most, and none on a whole number: "8.6 MB", "118 MB".
func megabytes(bytes *int64) string {
	if bytes == nil {
		return "0 MB"
	}

	mb := float64(*bytes) / bytesInMB
	rounded := float64(int64(mb*10+0.5)) / 10

	p := message.NewPrinter(language.English)
	if rounded == float64(int64(rounded)) {
		return p.Sprintf("%.0f MB", rounded)
	}

	return p.Sprintf("%.1f MB", rounded)
}

func duration(d time.Duration) string {
	if d <= 0 {
		return ""
	}

	seconds := d.Seconds()

	return fmt.Sprintf("%dm %04.1fs", int(seconds/60), math.Mod(seconds, 60))
}
