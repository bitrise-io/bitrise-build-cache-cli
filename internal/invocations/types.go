package invocations

import (
	"encoding/json"
	"time"
)

// Build-tool identifiers accepted by the bitrise-website API.
const (
	BuildToolGradle      = "gradle"
	BuildToolBazel       = "bazel"
	BuildToolXcode       = "xcode"
	BuildToolReactNative = "react-native"
	BuildToolCcache      = "ccache"
)

// Status values for the list filter.
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// Order-by values.
const (
	OrderByStartedAt = "started_at"
	OrderByCacheHit  = "cache_hit"
	OrderByDuration  = "duration"
)

// Order-direction values.
const (
	OrderAscending  = "ascending"
	OrderDescending = "descending"
)

// Paging matches the `paging` key in the list response.
type Paging struct {
	Page       int `json:"page"`
	PerPage    int `json:"perPage"`
	TotalCount int `json:"totalCount"`
}

// ListResponse is returned by GET /build-cache/:ws/invocations.json.
// Items are presenter hashes — keep as RawMessage so callers can decode
// into their preferred shape, or use the convenience InvocationSummary.
type ListResponse struct {
	Items  []json.RawMessage `json:"items"`
	Paging Paging            `json:"paging"`
}

// InvocationSummary mirrors the JSON shape produced by
// `BuildToolInvocationInfoPresenter#to_h` in
// `bitrise-io/bitrise-website` →
// `components/cache/app/presenters/build_tool_invocation_info_presenter.rb`.
//
// ACI-4914: this is the pinned contract for items in the list response and
// for the body of the show response. Field names + types follow the presenter
// exactly. Conditional fields are tagged `omitempty` and documented inline so
// consumers know when to expect them.
//
// **Pinned, always-present fields (presenter `to_h` head):**
//
//	projectSlug, projectTitle, buildSlug, cacheHitRate, ciProvider, command,
//	shortCommand, commitHash, duration, invocationId, repositoryUrl,
//	startedAt (ISO 8601 string), stepExecutionId, status, tool, toolVersion,
//	toolBuildNumber, toolWithVersionInfo, usesCache, workflowName,
//	hasMeaningfulExecutionReasons, hasAnyChildInvocation
//
// **Conditional fields** (added by the presenter only when the underlying
// data is present — see `to_h` body): benchmarkPhase, wrapper,
// parentInvocation, failedTaskPath, failureReason (gradle), targetOs (RN),
// buildToolStats, ccacheDiagnostics (ccache), bazelTotalActionCount,
// bazelCachedActionCount (bazel), gradleCriticalPath, taskStatistics
// (gradle), gitRepositoryTitle, gitRepositoryWebUrl, gitRepositoryProvider,
// buildNumber, branchName, branchUrl.
//
// **Notes for Go consumers:**
//   - `tool` is the canonical build-tool key (gradle / bazel / xcode /
//     react-native / ccache); `BuildToolXxx` constants in this package match.
//   - `status` is a string, not a bool; check against `StatusSuccess` /
//     `StatusFailed` rather than expecting a `success` bool.
//   - `duration` is whatever the underlying invocation object stores — unit
//     not normalised by the presenter. Treat it as opaque numeric and do not
//     assume milliseconds.
//   - `startedAt` is an ISO 8601 string; the Go `time.Time` decoder accepts
//     RFC 3339 which covers the same set of values produced by Ruby's
//     `iso8601`.
//   - `cacheHitRate` may be `null` in JSON; we model it as `*float64` so
//     consumers can distinguish "no data" from "0.0". `usesCache` is the
//     boolean derived form (`cacheHitRate > 0`).
//   - User identity (commitEmail, hostname, etc.) is **NOT** in the presenter
//     today. Filter for those server-side via the new xcode-analytics-service
//     params (ACI-4908 / ACI-4909) instead of expecting them in the response.
type InvocationSummary struct {
	// Pinned head — always present.
	InvocationID                  string    `json:"invocationId"`
	Tool                          string    `json:"tool"`
	ProjectSlug                   string    `json:"projectSlug"`
	ProjectTitle                  string    `json:"projectTitle"`
	BuildSlug                     string    `json:"buildSlug"`
	RepositoryURL                 string    `json:"repositoryUrl"`
	CommitHash                    string    `json:"commitHash"`
	WorkflowName                  string    `json:"workflowName"`
	CIProvider                    string    `json:"ciProvider"`
	Command                       string    `json:"command"`
	ShortCommand                  string    `json:"shortCommand"`
	StartedAt                     time.Time `json:"startedAt"`
	StepExecutionID               string    `json:"stepExecutionId"`
	Status                        string    `json:"status"`
	ToolVersion                   string    `json:"toolVersion"`
	ToolBuildNumber               string    `json:"toolBuildNumber"`
	ToolWithVersionInfo           string    `json:"toolWithVersionInfo"`
	Duration                      *float64  `json:"duration,omitempty"`
	CacheHitRate                  *float64  `json:"cacheHitRate"`
	UsesCache                     bool      `json:"usesCache"`
	HasMeaningfulExecutionReasons *bool     `json:"hasMeaningfulExecutionReasons"` // nullable in prod responses
	HasAnyChildInvocation         bool      `json:"hasAnyChildInvocation"`

	// Conditional — populated by the presenter only when present in source.
	BenchmarkPhase         string                `json:"benchmarkPhase,omitempty"`
	Wrapper                string                `json:"wrapper,omitempty"`
	ParentInvocation       *ParentInvocationRef  `json:"parentInvocation,omitempty"`
	FailedTaskPath         string                `json:"failedTaskPath,omitempty"`
	FailureReason          string                `json:"failureReason,omitempty"`
	TargetOS               []string              `json:"targetOs,omitempty"`
	BazelTotalActionCount  *int                  `json:"bazelTotalActionCount,omitempty"`
	BazelCachedActionCount *int                  `json:"bazelCachedActionCount,omitempty"`
	TaskStatistics         *GradleTaskStatistics `json:"taskStatistics,omitempty"`
	GitRepositoryTitle     string                `json:"gitRepositoryTitle,omitempty"`
	GitRepositoryWebURL    string                `json:"gitRepositoryWebUrl,omitempty"`
	GitRepositoryProvider  string                `json:"gitRepositoryProvider,omitempty"`
	BuildNumber            *int64                `json:"buildNumber,omitempty"`
	BranchName             string                `json:"branchName,omitempty"`
	BranchURL              string                `json:"branchUrl,omitempty"`
}

// ParentInvocationRef is the shape of `parentInvocation` on
// InvocationSummary, set by the presenter only when the invocation has a
// parent (currently RN child invocations).
type ParentInvocationRef struct {
	InvocationID string `json:"invocationId"`
	BuildTool    string `json:"buildTool"`
	Command      string `json:"command"`
}

// GradleTaskStatistics is the shape of `taskStatistics` set by the presenter
// for Gradle invocations that report task stats.
type GradleTaskStatistics struct {
	TotalTaskCount      int `json:"totalTaskCount"`
	UpToDateTaskCount   int `json:"upToDateTaskCount"`
	FromCacheTaskCount  int `json:"fromCacheTaskCount"`
	ActionableTaskCount int `json:"actionableTaskCount"`
	CacheableTaskCount  int `json:"cacheableTaskCount"`
}

// GradleTask is a row from GET .../invocations/gradle/:id/tasks.json.
type GradleTask struct {
	Path       string  `json:"path"`
	Outcome    string  `json:"outcome"`
	DurationMs int64   `json:"durationMs"`
	Cacheable  bool    `json:"cacheable"`
	CacheHit   bool    `json:"cacheHit"`
	HitRate    float32 `json:"hitRate,omitempty"`
}

// GradleTasksResponse matches the JSON of get_gradle_tasks.
type GradleTasksResponse struct {
	Tasks            []GradleTask `json:"tasks"`
	TotalCount       int          `json:"total_count"`
	TotalCachedCount int          `json:"total_cached_count"`
}

// BazelTarget is a row from GET .../invocations/bazel/:id/targets.json.
type BazelTarget struct {
	Label      string `json:"label"`
	Kind       string `json:"kind"`
	Outcome    string `json:"outcome"`
	DurationMs int64  `json:"durationMs"`
	CacheHit   bool   `json:"cacheHit"`
}

// BazelTargetsResponse matches the JSON of get_bazel_targets.
type BazelTargetsResponse struct {
	Targets []BazelTarget `json:"targets"`
}

// ChildInvocationGroup is a buildTool-grouped slice in
// get_child_invocations / get_sibling_invocations responses.
type ChildInvocationGroup struct {
	BuildTool   string              `json:"buildTool"`
	Invocations []InvocationSummary `json:"invocations"`
}

// ListFilter mirrors the query params accepted by the index action.
// Zero-valued fields are omitted from the query.
type ListFilter struct {
	Tool           string
	Page           int
	ItemsPerPage   int
	OrderBy        string
	OrderDirection string
	ProjectSlug    string
	BuildSlug      string
	RepositoryURL  string
	Workflow       string
	CIProvider     string
	Status         string
	Command        string
	Before         time.Time
	After          time.Time
}
