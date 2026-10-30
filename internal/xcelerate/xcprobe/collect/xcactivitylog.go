package collect

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/slf"
)

// ActivityLogCollector reads one `.xcactivitylog` for the invocation, decodes
// it into the SLF BuildStep tree, and projects it onto the EnrichmentPayload's
// target / step-histogram / top-steps rows plus (optionally) the Phase 2
// CompilationCacheMetrics aggregate.
type ActivityLogCollector struct{}

func (ActivityLogCollector) Name() string { return "xcactivitylog" }

const (
	defaultTopN      = 25
	logWindowPadding = 90 * time.Second
)

func (ActivityLogCollector) Collect(_ context.Context, in Input) (Output, error) {
	path, err := resolveActivityLogPath(in)
	if err != nil {
		return Output{}, err
	}

	if path == "" {
		return Output{}, nil
	}

	f, err := os.Open(path) //nolint:gosec // path discovered under in.DerivedDataPath or supplied by caller
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Output{}, nil
		}

		return Output{}, fmt.Errorf("open activity log: %w", err)
	}
	defer f.Close()

	root, _, err := slf.Decode(f)
	if err != nil {
		return Output{}, fmt.Errorf("decode activity log %s: %w", path, err)
	}

	return projectBuildStep(root, in), nil
}

func projectBuildStep(root *slf.BuildStep, in Input) Output {
	targetTopN := pickTopN(in.TargetTopN)
	stepTopN := pickTopN(in.StepTopN)
	swiftTopN := pickTopN(in.SwiftTopN)

	out := Output{}

	out.Targets = extractTargets(root, targetTopN)
	out.StepTypeHist = buildStepTypeHistogram(root)
	out.TopSteps = extractTopSteps(root, stepTopN)
	out.TopSwiftFuncs, out.TopSwiftTypes = extractSwiftTimings(root, swiftTopN)
	out.LinkerStats = extractLinkerStats(root)

	if in.DecodeAttachments {
		if ccm := aggregateCCM(root); ccm != nil {
			out.AttachmentsCCM = ccm
		}
	}

	return out
}

func pickTopN(v int) int {
	if v <= 0 {
		return defaultTopN
	}

	return v
}

// resolveActivityLogPath honours a caller-supplied path first; otherwise
// walks DerivedData for the newest .xcactivitylog whose mtime falls inside
// the invocation window (padded).
func resolveActivityLogPath(in Input) (string, error) {
	if in.ActivityLogPath != "" {
		return in.ActivityLogPath, nil
	}

	if in.DerivedDataPath == "" {
		return "", nil
	}

	logsRoot := filepath.Join(in.DerivedDataPath, "Logs", "Build")

	// Fall back to the entire DerivedData when the Logs/Build subdir is
	// missing — the caller may have handed us the logs directory directly.
	if _, err := os.Stat(logsRoot); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat logs dir: %w", err)
		}

		logsRoot = in.DerivedDataPath
	}

	entries, err := os.ReadDir(logsRoot)
	if err != nil {
		return "", nil //nolint:nilerr // missing DerivedData is a no-op, not an error
	}

	start := in.WindowStart.Add(-logWindowPadding)
	end := in.WindowEnd.Add(logWindowPadding)

	var newest string

	var newestMod time.Time

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".xcactivitylog") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		mod := info.ModTime()
		if !in.WindowStart.IsZero() && mod.Before(start) {
			continue
		}

		if !in.WindowEnd.IsZero() && mod.After(end) {
			continue
		}

		if mod.After(newestMod) {
			newestMod = mod
			newest = filepath.Join(logsRoot, e.Name())
		}
	}

	return newest, nil
}

// extractTargets walks the root's immediate children (plus any descendants
// whose domain starts with `.target`) and projects one TargetRow per target
// section, capped at targetTopN by duration.
func extractTargets(root *slf.BuildStep, targetTopN int) []payload.TargetRow {
	var rows []payload.TargetRow

	var walk func(b *slf.BuildStep)
	walk = func(b *slf.BuildStep) {
		if b.IsTargetStep() {
			row := payload.TargetRow{
				Name:                  cleanTargetName(b.Title),
				DurationMs:            durationMs(b.TimeStartedRecording, b.TimeStoppedRecording),
				CompilationDurationMs: computeCompilationDurationMs(b),
				FetchedFromCache:      b.WasFetchedFromCache,
				Warnings:              b.WarningCount(),
				Errors:                b.ErrorCount(),
				StepCount:             countDetailSteps(b),
			}

			rows = append(rows, row)

			return
		}

		for _, s := range b.SubSteps {
			walk(s)
		}
	}

	walk(root)

	sort.Slice(rows, func(i, j int) bool { return rows[i].DurationMs > rows[j].DurationMs })

	if len(rows) > targetTopN {
		rows = rows[:targetTopN]
	}

	return rows
}

// cleanTargetName trims the "Build target " prefix Xcode wraps target titles
// in on the activity log.
func cleanTargetName(raw string) string {
	for _, prefix := range []string{"Build target ", "Prepare build for ", "Compile target "} {
		if strings.HasPrefix(raw, prefix) {
			return strings.TrimPrefix(raw, prefix)
		}
	}

	return raw
}

func countDetailSteps(b *slf.BuildStep) int {
	n := 0

	for _, s := range b.SubSteps {
		if s.ClassName == slf.ClassIDEActivityLogCommandInvocationSection {
			n++
		}

		n += countDetailSteps(s)
	}

	return n
}

// computeCompilationDurationMs returns the span from the target's start to
// the latest compilation substep's end. Mirrors XCMetrics' per-target
// compilation_duration split (what wall-clock the compilations occupied
// vs the whole-target duration that trails into linking/signing).
func computeCompilationDurationMs(target *slf.BuildStep) int64 {
	var compilationEnd float64

	var walk func(b *slf.BuildStep)
	walk = func(b *slf.BuildStep) {
		switch b.DetailStepType() {
		case "cCompilation", "swiftCompilation", "swiftAggregatedCompilation",
			"compileAssetsCatalog", "compileStoryboard":
			if b.TimeStoppedRecording > compilationEnd {
				compilationEnd = b.TimeStoppedRecording
			}
		}

		for _, s := range b.SubSteps {
			walk(s)
		}
	}

	walk(target)

	if compilationEnd <= target.TimeStartedRecording {
		return 0
	}

	return durationSecsToMs(compilationEnd - target.TimeStartedRecording)
}

// buildStepTypeHistogram counts leaf command invocations per DetailStepType
// and sums their duration. Mirrors the shape BuildTimingCollector produces
// from -showBuildTimingSummary but driven by the SLF tree, so it works on
// the wrapperless/IDE path too.
func buildStepTypeHistogram(root *slf.BuildStep) []payload.StepTypeBucket {
	totals := map[string]*payload.StepTypeBucket{}

	var walk func(b *slf.BuildStep)
	walk = func(b *slf.BuildStep) {
		if b.ClassName == slf.ClassIDEActivityLogCommandInvocationSection {
			stepType := b.DetailStepType()

			bucket, ok := totals[stepType]
			if !ok {
				bucket = &payload.StepTypeBucket{Type: stepType}
				totals[stepType] = bucket
			}

			bucket.Count++
			bucket.TotalMs += durationMs(b.TimeStartedRecording, b.TimeStoppedRecording)
		}

		for _, s := range b.SubSteps {
			walk(s)
		}
	}

	walk(root)

	out := make([]payload.StepTypeBucket, 0, len(totals))
	for _, v := range totals {
		out = append(out, *v)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].TotalMs > out[j].TotalMs })

	return out
}

// extractTopSteps collects every leaf command-invocation step, sorts by
// duration descending, and caps at stepTopN. The parent-target name is
// derived during the walk so the top-steps row carries context without
// requiring a second traversal.
func extractTopSteps(root *slf.BuildStep, stepTopN int) []payload.StepRow {
	var rows []payload.StepRow

	var walk func(b *slf.BuildStep, target string)
	walk = func(b *slf.BuildStep, target string) {
		currentTarget := target
		if b.IsTargetStep() {
			currentTarget = cleanTargetName(b.Title)
		}

		if b.ClassName == slf.ClassIDEActivityLogCommandInvocationSection {
			rows = append(rows, payload.StepRow{
				Type:       b.DetailStepType(),
				Title:      truncate(b.Title, 240),
				Target:     currentTarget,
				DurationMs: durationMs(b.TimeStartedRecording, b.TimeStoppedRecording),
			})
		}

		for _, s := range b.SubSteps {
			walk(s, currentTarget)
		}
	}

	walk(root, "")

	sort.Slice(rows, func(i, j int) bool { return rows[i].DurationMs > rows[j].DurationMs })

	if len(rows) > stepTopN {
		rows = rows[:stepTopN]
	}

	return rows
}

// extractSwiftTimings is a Phase-3 stub: Swift function-body and expression
// type-check timings are parsed out of a step's Text / Messages fields when
// the matching -Xfrontend flag was passed to the compiler. Phase 1 scans
// Messages for the "slow swift expression" / "slow swift function" shape
// that XCLogParser extracts via regex. When neither flag is on, this
// returns empty slices — not an error.
func extractSwiftTimings(root *slf.BuildStep, swiftTopN int) ([]payload.SwiftTiming, []payload.SwiftTiming) {
	var funcs, types []payload.SwiftTiming

	// Live-stream regex-shaped parsing is Phase 3; for now we only capture
	// Swift timings that Apple added to the SLF tree directly. On today's
	// Xcode majors those are not present without flag-injection, so this
	// function is a scaffold that the Phase-3 flag-injection path will
	// populate once wired up. Returning the Top-N slice capped-and-sorted
	// keeps the collector contract shape-stable in the meantime.

	_ = root

	sort.Slice(funcs, func(i, j int) bool { return funcs[i].DurationMs > funcs[j].DurationMs })
	sort.Slice(types, func(i, j int) bool { return types[i].DurationMs > types[j].DurationMs })

	if len(funcs) > swiftTopN {
		funcs = funcs[:swiftTopN]
	}

	if len(types) > swiftTopN {
		types = types[:swiftTopN]
	}

	return funcs, types
}

// extractLinkerStats scans for a step whose signature indicates ld64 with
// -print_statistics. The structured LinkerStatistics lives in the step's
// stderr text when the flag is enabled; without the flag the field stays
// nil. Phase 1 is a stub that returns nil until the Phase-3
// flag-injection path lands.
func extractLinkerStats(root *slf.BuildStep) *payload.LinkerStatistics {
	_ = root

	return nil
}

// aggregateCCM sums BuildOperationMetrics counters across every attachment
// in the tree. Returns nil when the tree carries zero attachments or no
// counters survived decode.
func aggregateCCM(root *slf.BuildStep) *payload.CompilationCacheMetrics {
	agg := payload.CompilationCacheMetrics{
		Counters:     map[string]int64{},
		TaskCounters: map[string]map[string]int64{},
	}

	var walk func(b *slf.BuildStep)
	walk = func(b *slf.BuildStep) {
		for _, a := range b.Attachments {
			if a.BuildOp == nil {
				continue
			}

			agg.Attachments++

			for k, v := range a.BuildOp.Counters {
				switch k {
				case "clangCacheHits":
					agg.ClangCacheHits += v
				case "clangCacheMisses":
					agg.ClangCacheMisses += v
				case "swiftCacheHits":
					agg.SwiftCacheHits += v
				case "swiftCacheMisses":
					agg.SwiftCacheMisses += v
				default:
					agg.Counters[k] += v
				}
			}

			for k, inner := range a.BuildOp.TaskCounters {
				if agg.TaskCounters[k] == nil {
					agg.TaskCounters[k] = map[string]int64{}
				}

				for ik, iv := range inner {
					agg.TaskCounters[k][ik] += iv
				}
			}
		}

		for _, s := range b.SubSteps {
			walk(s)
		}
	}

	walk(root)

	if agg.Attachments == 0 {
		return nil
	}

	if len(agg.Counters) == 0 {
		agg.Counters = nil
	}

	if len(agg.TaskCounters) == 0 {
		agg.TaskCounters = nil
	}

	return &agg
}

func durationMs(start, stop float64) int64 {
	d := stop - start
	if d <= 0 {
		return 0
	}

	return durationSecsToMs(d)
}

func durationSecsToMs(d float64) int64 {
	return int64(math.Round(d * 1000.0))
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	return s[:limit]
}
