package collect

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// BuildTimingCollector parses the tail of `xcodebuild
// -showBuildTimingSummary` into one bucket per observed task type.
type BuildTimingCollector struct{}

func (BuildTimingCollector) Name() string { return "buildtiming" }

// knownBuildTaskTypes is an allow-list bounding BE row cardinality: unknown
// task types are dropped rather than creating a new bucket per unseen type.
var knownBuildTaskTypes = []string{ //nolint:gochecknoglobals
	"CompileSwift",
	"CompileC",
	"Ld",
	"PhaseScriptExecution",
	"CodeSign",
	"CopySwiftLibs",
	"CompileAssetCatalog",
	"CompileStoryboard",
}

// buildTimingLineRe matches rows like `CompileSwift (1234 tasks) | 56.789 seconds`.
var buildTimingLineRe = regexp.MustCompile( //nolint:gochecknoglobals
	`^\s*([A-Za-z][A-Za-z0-9_]+)\s+\((\d+)\s+tasks?\)\s*\|\s*(\d+\.?\d*)\s*seconds\s*$`,
)

func (BuildTimingCollector) Collect(_ context.Context, in Input) (Output, error) {
	if len(in.XcodebuildInfoBytes) == 0 {
		return Output{}, nil
	}

	buckets := map[string]payload.BuildTimingBucket{}

	for _, line := range strings.Split(string(in.XcodebuildInfoBytes), "\n") {
		m := buildTimingLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		taskType := m[1]
		if !isKnownBuildTaskType(taskType) {
			continue
		}

		count, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}

		seconds, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			continue
		}

		existing := buckets[taskType]
		existing.Count += count
		existing.TotalSeconds += seconds
		buckets[taskType] = existing
	}

	if len(buckets) == 0 {
		return Output{}, nil
	}

	return Output{BuildTimings: &payload.BuildTimingHistogram{Buckets: buckets}}, nil
}

func isKnownBuildTaskType(name string) bool {
	for _, k := range knownBuildTaskTypes {
		if name == k {
			return true
		}
	}

	return false
}
