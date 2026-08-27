// Package xcresult parses a subset of `xcrun xcresulttool get build-results`
// output into a small, storage-neutral summary used by the xcodebuild wrapper
// to enrich the invocation PUT with target names, per-target build durations,
// and failure snippets.
package xcresult

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	// MaxBundleSize caps the bundle size we're willing to hand to xcresulttool.
	// Beyond this we skip the parse — a giant xcresult usually means the build
	// wrote per-test artifacts that xcresulttool would spend seconds walking.
	MaxBundleSize = 200 * 1024 * 1024

	// parseTimeout bounds a single xcresulttool invocation. Shell-out is a fallback
	// enrichment path; if it hangs we drop the attachments rather than stall the
	// wrapper's PUT.
	parseTimeout = 10 * time.Second

	// stderrCap bounds the stderr buffer we retain for diagnostics.
	stderrCap = 1024 * 1024
)

// Summary is the wrapper-side view of a Xcode 26 build's xcresult.
type Summary struct {
	Targets  []TargetSummary
	Failures []FailureSummary
}

type TargetSummary struct {
	Name            string
	BuildDurationMs int64
}

type FailureSummary struct {
	TargetName string
	Message    string
}

// Parser wraps xcresulttool + JSON decoding. It's a small interface so tests
// can seam the shell-out without spawning a subprocess.
type Parser interface {
	Parse(ctx context.Context, bundlePath string) (Summary, error)
}

// DefaultParser is the production parser. XcrunPath is optional — when empty
// the shell PATH resolves xcrun.
type DefaultParser struct {
	XcrunPath   string
	CommandFunc utils.CommandFunc
	Logger      log.Logger
}

// NewDefaultParser builds a parser with `xcrun` on PATH. Logger is optional —
// when nil, diagnostics go to a discard logger.
func NewDefaultParser(logger log.Logger) *DefaultParser {
	return &DefaultParser{
		XcrunPath:   "xcrun",
		CommandFunc: utils.DefaultCommandFunc(),
		Logger:      logger,
	}
}

// Parse invokes xcresulttool and returns a Summary. Every recoverable failure
// mode (missing binary, non-zero exit, JSON error, size cap, timeout) returns
// an empty Summary and a nil error so the caller can attach nothing without
// aborting the enclosing analytics PUT.
func (p *DefaultParser) Parse(ctx context.Context, bundlePath string) (Summary, error) {
	logger := p.logger()

	if bundlePath == "" {
		return Summary{}, nil
	}

	if size, ok := bundleSize(bundlePath); ok && size > MaxBundleSize {
		logger.Infof("xcresult bundle exceeds %d bytes (got %d), skipping enrichment", MaxBundleSize, size)

		return Summary{}, nil
	}

	xcrun := p.XcrunPath
	if xcrun == "" {
		xcrun = "xcrun"
	}

	if _, err := exec.LookPath(xcrun); err != nil {
		logger.Debugf("xcresult parse skipped, xcrun not found: %v", err)

		return Summary{}, nil
	}

	cmdFunc := p.CommandFunc
	if cmdFunc == nil {
		cmdFunc = utils.DefaultCommandFunc()
	}

	ctx, cancel := context.WithTimeout(ctx, parseTimeout)
	defer cancel()

	cmd := cmdFunc(ctx, xcrun, "xcresulttool", "get", "build-results", "--path", bundlePath, "--format", "json")

	out, err := cmd.CombinedOutput()
	if err != nil {
		snippet := truncate(string(out), stderrCap)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logger.Warnf("xcresulttool timed out after %s: %s", parseTimeout, snippet)
		} else {
			logger.Warnf("xcresulttool failed: %v (stderr: %s)", err, snippet)
		}

		return Summary{}, nil
	}

	summary, err := parseBuildResults(out)
	if err != nil {
		logger.Warnf("Failed to parse xcresult: %v", err)

		return Summary{}, nil
	}

	return summary, nil
}

func (p *DefaultParser) logger() log.Logger {
	if p.Logger != nil {
		return p.Logger
	}

	return log.NewLogger(log.WithOutput(io.Discard))
}

// bundleSize returns the total size of an xcresult bundle (a directory) or a
// single-file bundle. false when stat fails.
func bundleSize(path string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	if !info.IsDir() {
		return info.Size(), true
	}

	var total int64
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, false
	}
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		total += fi.Size()
	}

	return total, true
}

// buildResultsPayload is the narrow shape of xcresulttool's build-results JSON
// this parser consumes. Unknown fields are tolerated: we only extract a few
// fields and leave the rest unread.
type buildResultsPayload struct {
	AnalyzerWarningCount int           `json:"analyzerWarningCount,omitempty"`
	ErrorCount           int           `json:"errorCount,omitempty"`
	WarningCount         int           `json:"warningCount,omitempty"`
	Status               string        `json:"status,omitempty"`
	StartedTime          float64       `json:"startedTime,omitempty"`
	EndedTime            float64       `json:"endedTime,omitempty"`
	Errors               []issueEntry  `json:"errors,omitempty"`
	Warnings             []issueEntry  `json:"warnings,omitempty"`
	AnalyzerWarnings     []issueEntry  `json:"analyzerWarnings,omitempty"`
	Actions              []actionEntry `json:"actions,omitempty"`
	DestinationTargets   []targetEntry `json:"destinationTargets,omitempty"`
	Targets              []targetEntry `json:"targets,omitempty"`
	BuildMetrics         *buildMetrics `json:"buildMetrics,omitempty"`
	Runs                 []runEntry    `json:"runs,omitempty"`
	// Some xcresulttool builds nest the flat lists under "buildResult".
	BuildResult *buildResultsPayload `json:"buildResult,omitempty"`
}

type actionEntry struct {
	Title      string               `json:"title,omitempty"`
	SchemeName string               `json:"schemeName,omitempty"`
	Targets    []targetEntry        `json:"targets,omitempty"`
	Result     *buildResultsPayload `json:"result,omitempty"`
}

type runEntry struct {
	Targets []targetEntry `json:"targets,omitempty"`
}

type targetEntry struct {
	Name            string        `json:"name,omitempty"`
	TargetName      string        `json:"targetName,omitempty"`
	BuildDurationMs int64         `json:"buildDurationInMs,omitempty"`
	BuildDurationS  float64       `json:"buildDurationInSeconds,omitempty"`
	StartedTime     float64       `json:"startedTime,omitempty"`
	EndedTime       float64       `json:"endedTime,omitempty"`
	Metrics         *buildMetrics `json:"metrics,omitempty"`
}

type buildMetrics struct {
	BuildDurationMs int64   `json:"buildDurationInMs,omitempty"`
	BuildDurationS  float64 `json:"buildDurationInSeconds,omitempty"`
}

type issueEntry struct {
	Message    string `json:"message,omitempty"`
	IssueType  string `json:"issueType,omitempty"`
	TargetName string `json:"targetName,omitempty"`
	// Some xcresulttool outputs use "producingTarget" or nest a Reference.
	ProducingTarget *struct {
		TargetName string `json:"targetName,omitempty"`
	} `json:"producingTarget,omitempty"`
}

func parseBuildResults(raw []byte) (Summary, error) {
	trimmed := trimLeadingWhitespace(raw)
	if len(trimmed) == 0 {
		return Summary{}, nil
	}

	var payload buildResultsPayload
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return Summary{}, fmt.Errorf("unmarshal build-results: %w", err)
	}

	summary := Summary{}
	collectTargets(&summary, payload)
	collectFailures(&summary, payload)

	return summary, nil
}

// collectTargets walks every plausible location a target duration can appear
// in and de-dupes by name. Different xcresulttool versions expose targets at
// slightly different depths; we tolerate all of them rather than lock the
// wrapper to one shape.
func collectTargets(sum *Summary, payload buildResultsPayload) {
	seen := map[string]int{}

	add := func(entries []targetEntry) {
		for _, t := range entries {
			name := t.Name
			if name == "" {
				name = t.TargetName
			}
			if name == "" {
				continue
			}

			duration := targetDurationMs(t)
			if idx, ok := seen[name]; ok {
				if duration > sum.Targets[idx].BuildDurationMs {
					sum.Targets[idx].BuildDurationMs = duration
				}

				continue
			}

			seen[name] = len(sum.Targets)
			sum.Targets = append(sum.Targets, TargetSummary{
				Name:            name,
				BuildDurationMs: duration,
			})
		}
	}

	add(payload.Targets)
	add(payload.DestinationTargets)
	for _, a := range payload.Actions {
		add(a.Targets)
		if a.Result != nil {
			add(a.Result.Targets)
			add(a.Result.DestinationTargets)
		}
	}
	for _, r := range payload.Runs {
		add(r.Targets)
	}
	if payload.BuildResult != nil {
		add(payload.BuildResult.Targets)
		add(payload.BuildResult.DestinationTargets)
	}
}

func targetDurationMs(t targetEntry) int64 {
	if t.BuildDurationMs > 0 {
		return t.BuildDurationMs
	}
	if t.Metrics != nil && t.Metrics.BuildDurationMs > 0 {
		return t.Metrics.BuildDurationMs
	}
	if t.Metrics != nil && t.Metrics.BuildDurationS > 0 {
		return int64(t.Metrics.BuildDurationS * 1000)
	}
	if t.BuildDurationS > 0 {
		return int64(t.BuildDurationS * 1000)
	}
	if t.StartedTime > 0 && t.EndedTime > t.StartedTime {
		return int64((t.EndedTime - t.StartedTime) * 1000)
	}

	return 0
}

func collectFailures(sum *Summary, payload buildResultsPayload) {
	add := func(entries []issueEntry) {
		for _, e := range entries {
			msg := firstLine(e.Message)
			if msg == "" {
				continue
			}

			target := e.TargetName
			if target == "" && e.ProducingTarget != nil {
				target = e.ProducingTarget.TargetName
			}

			sum.Failures = append(sum.Failures, FailureSummary{
				TargetName: target,
				Message:    msg,
			})
		}
	}

	add(payload.Errors)
	for _, a := range payload.Actions {
		if a.Result != nil {
			add(a.Result.Errors)
		}
	}
	if payload.BuildResult != nil {
		add(payload.BuildResult.Errors)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}

	return s
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	return s[:limit]
}

func trimLeadingWhitespace(b []byte) []byte {
	for i, c := range b {
		if c != ' ' && c != '\n' && c != '\r' && c != '\t' {
			return b[i:]
		}
	}

	return nil
}
