package slf

import (
	"encoding/json"
	"strings"
)

// Attachment is one entry of the IDEActivityLogSectionAttachment array that
// appears on every IDEActivityLogSection starting with SLF v11 (Xcode 15.3+).
//
// The attachment carries one of three structured JSON payloads (TaskMetrics,
// TaskBacktrace, BuildOperationMetrics). We expose the raw JSON on
// Payload (for forward compatibility) plus typed structs for the shapes
// we understand so collectors can skip re-parsing.
type Attachment struct {
	Identifier   string
	MajorVersion uint64
	MinorVersion uint64

	// Payload is the raw JSON body as emitted by Xcode. Always populated.
	Payload json.RawMessage

	// Decoded structs — populated when the identifier matches a known shape
	// and the body parses cleanly. On parse failure the typed field stays nil
	// and only Payload survives.
	TaskMetrics   *TaskMetrics
	TaskBacktrace *TaskBacktrace
	BuildOp       *BuildOperationMetrics
}

// TaskMetrics mirrors IDEFoundation.IDEActivityLogSectionAttachment.BuildOperationTaskMetrics.
type TaskMetrics struct {
	UTime       uint64 `json:"utime,omitempty"`
	STime       uint64 `json:"stime,omitempty"`
	MaxRSS      uint64 `json:"maxRSS,omitempty"`
	WCStartTime uint64 `json:"wcStartTime,omitempty"`
	WCDuration  uint64 `json:"wcDuration,omitempty"`
}

// TaskBacktrace is the Xcode "why did we run this task" frames list.
// Shape per XCLogParser: an array of frames each carrying a single-key
// category object + description.
type TaskBacktrace struct {
	Frames []TaskBacktraceFrame `json:"frames,omitempty"`
}

// TaskBacktraceFrame is pass-through. The category JSON key is one of
// ruleHadInvalidValue / ruleSignatureChanged / ruleNeverBuilt /
// ruleInputRebuilt / ruleForced / dynamicTaskRegistration /
// dynamicTaskRequest / none; collectors can project it as-is.
type TaskBacktraceFrame struct {
	Category    json.RawMessage `json:"category,omitempty"`
	Description string          `json:"description,omitempty"`
}

// BuildOperationMetrics covers both the pre-Xcode-26.4 cache-counter format
// and the current counter/taskCounter dictionary format. Unknown numeric
// fields on the pre-26.4 format are flattened into Counters so a downstream
// aggregator does not need to know which Xcode wrote the log.
type BuildOperationMetrics struct {
	Counters     map[string]int64            `json:"counters,omitempty"`
	TaskCounters map[string]map[string]int64 `json:"taskCounters,omitempty"`
}

// CompilationCacheMetrics is the aggregate rollup emitted on
// EnrichmentPayload.AttachmentsCCM — summed across all attachment-bearing
// sections. The split between Clang and Swift mirrors the two attestor
// subsystems that Xcode reports in the BuildOperationMetrics attachment
// (Xcode 15.3 pre-26.4 format carries them as discrete counter keys).
type CompilationCacheMetrics struct {
	ClangCacheHits   int64 `json:"clangCacheHits,omitempty"`
	ClangCacheMisses int64 `json:"clangCacheMisses,omitempty"`
	SwiftCacheHits   int64 `json:"swiftCacheHits,omitempty"`
	SwiftCacheMisses int64 `json:"swiftCacheMisses,omitempty"`

	// Counters captures any additional numeric key found on recent
	// BuildOperationMetrics payloads so the BE sees the full picture even
	// on an Xcode major we haven't typed-up yet.
	Counters map[string]int64 `json:"counters,omitempty"`

	// TaskCounters passes through the nested dict Xcode 26.4+ writes under
	// `taskCounters`. Keys are task-category names.
	TaskCounters map[string]map[string]int64 `json:"taskCounters,omitempty"`

	// Attachments is the number of attachment-bearing sections rolled up
	// into this aggregate (one aggregate per invocation).
	Attachments int `json:"attachments,omitempty"`
}

// decodeAttachment inspects an attachment's identifier suffix and routes
// the JSON into the matching typed struct. Unknown identifiers leave the
// typed fields nil but still preserve Payload.
func decodeAttachment(a *Attachment) {
	suffix := attachmentSuffix(a.Identifier)
	if suffix == "" {
		return
	}

	switch suffix {
	case "TaskMetrics":
		var tm TaskMetrics
		if err := json.Unmarshal(a.Payload, &tm); err == nil {
			a.TaskMetrics = &tm
		}
	case "TaskBacktrace":
		var frames []TaskBacktraceFrame
		if err := json.Unmarshal(a.Payload, &frames); err == nil {
			a.TaskBacktrace = &TaskBacktrace{Frames: frames}
		}
	case "BuildOperationMetrics":
		if bom, ok := parseBuildOperationMetrics(a.Payload); ok {
			a.BuildOp = &bom
		}
	}
}

func attachmentSuffix(id string) string {
	i := strings.LastIndex(id, "ActivityLogSectionAttachment.")
	if i < 0 {
		return ""
	}

	return id[i+len("ActivityLogSectionAttachment."):]
}

// parseBuildOperationMetrics first tries the Xcode 26.4+ counter-dict shape
// then falls back to the pre-26.4 flat cache-counter shape, migrating the
// legacy keys into Counters so the typed consumer gets one representation.
func parseBuildOperationMetrics(body []byte) (BuildOperationMetrics, bool) {
	var direct BuildOperationMetrics
	if err := json.Unmarshal(body, &direct); err == nil && (len(direct.Counters) > 0 || len(direct.TaskCounters) > 0) {
		return direct, true
	}

	// Legacy flat counter shape.
	var legacy struct {
		ClangCacheHits   int64 `json:"clangCacheHits"`
		ClangCacheMisses int64 `json:"clangCacheMisses"`
		SwiftCacheHits   int64 `json:"swiftCacheHits"`
		SwiftCacheMisses int64 `json:"swiftCacheMisses"`
	}

	if err := json.Unmarshal(body, &legacy); err != nil {
		return BuildOperationMetrics{}, false
	}

	counters := map[string]int64{}

	if legacy.ClangCacheHits > 0 {
		counters["clangCacheHits"] = legacy.ClangCacheHits
	}

	if legacy.ClangCacheMisses > 0 {
		counters["clangCacheMisses"] = legacy.ClangCacheMisses
	}

	if legacy.SwiftCacheHits > 0 {
		counters["swiftCacheHits"] = legacy.SwiftCacheHits
	}

	if legacy.SwiftCacheMisses > 0 {
		counters["swiftCacheMisses"] = legacy.SwiftCacheMisses
	}

	if len(counters) == 0 {
		return BuildOperationMetrics{}, false
	}

	return BuildOperationMetrics{Counters: counters}, true
}
