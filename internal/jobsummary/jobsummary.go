// Package jobsummary renders a tool's end-of-build cache stats onto the GitHub
// Actions job page, from the counters the CLI already holds.
//
// GITHUB_STEP_SUMMARY is a file the runner creates for the step, and the CLI
// inherits it like any other environment variable, so this needs nothing added
// to the user's workflow.
//
// A block per invocation: its command, then a one-row table. Separate tables
// rather than one shared one, because the runner gives every step its own summary
// file and concatenates them for the job page -- rows can only accumulate within a
// step, so a single table would fragment across them anyway. The Gradle plugins
// write the same shape, so a mixed job reads as one list.
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

	heading = "### ⚡️ Bitrise Build Cache"

	annotationTitle = "Bitrise Build Cache"

	statusLowReuse = "Low reuse"

	tableHead = "| Status | Cache status | ↓ Downloaded | ↑ Uploaded | Duration | |\n" +
		"|:---:|---|---:|---:|---|---|\n"

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

// Block is the command, then a table of one: what the cache did for this invocation.
func Block(i Invocation) string {
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
		link = fmt.Sprintf("[View invocation](%s)", i.InvocationURL)
	}

	return fmt.Sprintf("**%s**\n\n%s| %s | %s | %s | %s | %s | %s |\n",
		command, tableHead, status, CacheStatus(i),
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

	return statusLowReuse
}

// Annotation is a one-line workflow command, which GitHub shows on the job run
// page as well as the pipeline summary -- the summary file only reaches the
// latter. Warning for low reuse, because that is the status worth acting on;
// everything else is a notice.
func Annotation(i Invocation) string {
	status := CacheStatus(i)

	level := "notice"
	if status == statusLowReuse {
		level = "warning"
	}

	command := i.Command
	if command == "" {
		command = "build"
	}

	view := ""
	if i.InvocationURL != "" {
		view = " View: " + i.InvocationURL
	}

	return fmt.Sprintf("::%s title=%s::%s — %s: %s downloaded, %s uploaded.%s",
		level, annotationTitle, status, command,
		megabytes(i.DownloadedBytes), megabytes(i.UploadedBytes), view)
}

// WriteAnnotation prints the workflow command straight to stdout, unprefixed:
// GitHub only reads a command that starts its line. Silent off GitHub Actions.
func WriteAnnotation(i Invocation) {
	if os.Getenv(summaryEnvVar) == "" {
		return
	}

	fmt.Fprintln(os.Stdout, Annotation(i))
}

// Write adds this invocation's block to the job summary, replacing it when the
// same invocation reports twice. Reports whether anything was written; a summary
// is a nicety, so no caller should fail on it.
func Write(block, section string) (bool, error) {
	path := os.Getenv(summaryEnvVar)
	if path == "" || block == "" {
		return false, nil
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", summaryEnvVar, err)
	}

	if err := os.WriteFile(path, []byte(withBlock(string(existing), block, section)), 0o644); err != nil { //nolint:gosec,mnd // the runner reads this file
		return false, fmt.Errorf("write %s: %w", summaryEnvVar, err)
	}

	return true, nil
}

func startMarker(section string) string { return "<!-- bitrise-build-cache:" + section + ":start -->" }

func endMarker(section string) string { return "<!-- bitrise-build-cache:" + section + ":end -->" }

func withBlock(content, block, section string) string {
	marked := startMarker(section) + "\n" + block + endMarker(section) + "\n\n"

	// One heading for the file, however many invocations write into it.
	if !strings.Contains(content, heading) {
		content += heading + "\n\n"
	}

	start := strings.Index(content, startMarker(section))
	if start < 0 {
		return content + marked
	}

	after := ""
	if end := strings.Index(content, endMarker(section)); end > start {
		after = strings.TrimLeft(content[end+len(endMarker(section)):], "\n")
	}

	return content[:start] + marked + after
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
