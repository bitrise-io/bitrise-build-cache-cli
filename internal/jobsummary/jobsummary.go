// Package jobsummary renders a tool's end-of-build cache stats onto the GitHub
// Actions job page, from the counters the CLI already holds.
//
// GITHUB_STEP_SUMMARY is a file the runner creates for the step, and the CLI
// inherits it like any other environment variable, so this needs nothing added
// to the user's workflow. Each tool owns a marked section it rewrites in place,
// which keeps one section per tool when a job builds more than once, and leaves
// the Gradle plugins' own section and anything another step wrote alone.
package jobsummary

import (
	"fmt"
	"os"
	"strings"

	"github.com/dustin/go-humanize"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

const summaryEnvVar = "GITHUB_STEP_SUMMARY"

// Summary is one tool's contribution to the job page.
type Summary struct {
	// Tool names the section, e.g. "Xcode" or "ccache".
	Tool string
	// Section keys the markers, so a rerun replaces rather than appends.
	Section string
	// Unit names what Hits and Total count: "tasks" for Xcode, "compilations"
	// for ccache. The two tools count different things and say so.
	Unit          string
	Hits          int64
	Total         int64
	BlobStats     *blobstats.Snapshot
	InvocationURL string
}

// Render returns the markdown, or "" when there is nothing worth showing.
func (s Summary) Render() string {
	hasWork := s.Total > 0
	hasTransfer := s.BlobStats != nil && !s.BlobStats.IsEmpty()
	if !hasWork && !hasTransfer {
		return ""
	}

	var b strings.Builder

	fmt.Fprintf(&b, "## 🤖 Bitrise Build Cache — %s\n\n", s.Tool)

	if hasWork {
		fmt.Fprintf(&b, "### %.1f%% of %s work avoided\n\n", float64(s.Hits)/float64(s.Total)*100, s.Tool)
		fmt.Fprintf(&b, "| | %s |\n| --- | ---: |\n", s.Unit)
		fmt.Fprintf(&b, "| From cache | %s |\n", humanize.Comma(s.Hits))
		fmt.Fprintf(&b, "| Executed | %s |\n", humanize.Comma(max(s.Total-s.Hits, 0)))
		fmt.Fprintf(&b, "| **Total** | **%s** |\n\n", humanize.Comma(s.Total))
	}

	if hasTransfer {
		b.WriteString(transferTable(s.BlobStats))
	}

	if s.InvocationURL != "" {
		fmt.Fprintf(&b, "[View the full invocation](%s)\n\n", s.InvocationURL)
	}

	return b.String()
}

func transferTable(snapshot *blobstats.Snapshot) string {
	var b strings.Builder

	b.WriteString("### Cache transfer\n\n")
	b.WriteString("| | download | upload |\n| --- | ---: | ---: |\n")
	fmt.Fprintf(&b, "| Blobs | %s | %s |\n",
		humanize.Comma(snapshot.Download.OpCount), humanize.Comma(snapshot.Upload.OpCount))
	fmt.Fprintf(&b, "| Bytes | %s | %s |\n",
		formatBytes(snapshot.Download.BytesTotal), formatBytes(snapshot.Upload.BytesTotal))
	fmt.Fprintf(&b, "| Latency p50 | %s | %s |\n",
		latency(snapshot.Download, 0.50), latency(snapshot.Upload, 0.50))
	fmt.Fprintf(&b, "| Latency p90 | %s | %s |\n\n",
		latency(snapshot.Download, 0.90), latency(snapshot.Upload, 0.90))
	b.WriteString("<sub>Latency percentiles are histogram bucket bounds, so they are approximate.</sub>\n\n")

	return b.String()
}

// Same bucket bounds the profile log line reports, so the page and the log agree.
func latency(d blobstats.DirectionSnapshot, quantile float64) string {
	if d.OpCount == 0 {
		return "—"
	}

	bound, overflow := d.LatencyMs.PercentileBucket(quantile)
	if overflow {
		return fmt.Sprintf(">%d ms", bound)
	}

	return fmt.Sprintf("%d ms", bound)
}

func formatBytes(n int64) string {
	if n < 0 {
		return "0 B"
	}

	return humanize.Bytes(uint64(n))
}

// Write replaces this tool's section of the job summary. Reports whether
// anything was written; a summary is a nicety, so no caller should fail on it.
func Write(section, markdown string) (bool, error) {
	path := os.Getenv(summaryEnvVar)
	if path == "" || markdown == "" {
		return false, nil
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", summaryEnvVar, err)
	}

	content := withoutSection(string(existing), section) +
		startMarker(section) + "\n" + markdown + endMarker(section) + "\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec,mnd // the runner reads this file
		return false, fmt.Errorf("write %s: %w", summaryEnvVar, err)
	}

	return true, nil
}

func startMarker(section string) string { return "<!-- bitrise-build-cache:" + section + ":start -->" }

func endMarker(section string) string { return "<!-- bitrise-build-cache:" + section + ":end -->" }

func withoutSection(content, section string) string {
	start := strings.Index(content, startMarker(section))
	if start < 0 {
		return content
	}

	end := strings.Index(content, endMarker(section))
	if end < start {
		return content[:start]
	}

	return content[:start] + content[end+len(endMarker(section)):]
}
