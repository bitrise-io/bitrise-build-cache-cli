// Package jobsummary puts a tool's cache stats on the GitHub Actions job page.
//
// GITHUB_STEP_SUMMARY is a file the runner creates for the step and the CLI
// inherits, so this needs nothing added to the user's workflow. A block per
// invocation rather than one shared table: the runner gives every step its own
// file, so a table could never span them. The Gradle plugins write the same
// shape.
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

	statusLowReuse = "Low reuse"

	tableHead = "| Status | Cache status | ↓ Downloaded | ↑ Uploaded | Duration | |\n" +
		"|:---:|---|---:|---:|---|---|\n"

	phaseBaseline = "baseline"
	phaseWarmup   = "warmup"

	bytesInMB = 1000 * 1000
)

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

// CacheStatus is the ladder invocationCacheStatus.ts applies in bitrise-website,
// so a row here and a row there read the same.
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

// Annotation reaches the job run page, which the summary file does not. Low reuse
// warns because it is the status worth acting on; everything else is a notice.
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
		view = " | View on Bitrise: " + i.InvocationURL
	}

	return fmt.Sprintf("::%s title=%s::Cache status: %s | %s downloaded, %s uploaded%s",
		level, escapeProperty(command), status,
		megabytes(i.DownloadedBytes), megabytes(i.UploadedBytes), view)
}

// A scheme or task path carries colons, which end a command's property list.
func escapeProperty(value string) string {
	return strings.NewReplacer(
		"%", "%25",
		"\r", "%0D",
		"\n", "%0A",
		":", "%3A",
		",", "%2C",
	).Replace(value)
}

// WriteAnnotation prints the workflow command straight to stdout, unprefixed:
// GitHub only reads a command that starts its line. Silent off GitHub Actions.
func WriteAnnotation(i Invocation) {
	if os.Getenv(summaryEnvVar) == "" {
		return
	}

	fmt.Fprintln(os.Stdout, Annotation(i))
}

// Write adds this invocation's block, replacing it when the same invocation
// reports twice. A summary is a nicety, so no caller should fail on it.
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
