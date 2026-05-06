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

// InvocationSummary covers the common fields produced by
// BuildToolInvocationInfoPresenter.to_h. Decode an Items entry into this
// when you only need the headline data.
type InvocationSummary struct {
	InvocationID    string    `json:"invocationId"`
	BuildTool       string    `json:"buildTool"`
	Tool            string    `json:"tool"`
	WorkspaceSlug   string    `json:"workspaceSlug"`
	AppSlug         string    `json:"appSlug"`
	BuildSlug       string    `json:"buildSlug"`
	RepositoryURL   string    `json:"repositoryUrl"`
	Branch          string    `json:"branch"`
	CommitHash      string    `json:"commitHash"`
	WorkflowName    string    `json:"workflowName"`
	CIProvider      string    `json:"ciProvider"`
	Command         string    `json:"command"`
	Success         bool      `json:"success"`
	StartedAt       time.Time `json:"startedAt"`
	DurationMs      int64     `json:"durationMs"`
	CacheHitRate    float32   `json:"cacheHitRate"`
	UserName        string    `json:"userName"`
	UserEmail       string    `json:"userEmail"`
	BitriseStepID   string    `json:"bitriseStepId,omitempty"`
	ProjectTitle    string    `json:"projectTitle,omitempty"`
	ParentInvocation *struct {
		InvocationID string `json:"invocationId"`
		BuildTool    string `json:"buildTool"`
	} `json:"parentInvocation,omitempty"`
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
	BuildTool   string            `json:"buildTool"`
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
