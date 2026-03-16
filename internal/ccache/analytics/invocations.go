package analytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

// rawCcacheStats mirrors the JSON structure emitted by `ccache --print-stats --format=json`.
type rawCcacheStats struct {
	Stats struct {
		CacheHitDirect       int     `json:"cache_hit_direct"`
		CacheHitPreprocessed int     `json:"cache_hit_preprocessed"`
		CacheMiss            int     `json:"cache_miss"`
		CacheHitRate         float64 `json:"cache_hit_rate"`
		ErrorsCompiling      int     `json:"errors_compiling"`
		FilesInCache         int     `json:"files_in_cache"`
		CacheSizeKibibyte    int64   `json:"cache_size_kibibyte"`
	} `json:"stats"`
}

// ParseCcacheStats parses the JSON output of `ccache --print-stats --format=json`.
func ParseCcacheStats(data []byte) (CcacheStats, error) {
	var raw rawCcacheStats
	if err := json.Unmarshal(data, &raw); err != nil {
		return CcacheStats{}, fmt.Errorf("parse ccache stats JSON: %w", err)
	}

	return CcacheStats{
		CacheHitDirect:       raw.Stats.CacheHitDirect,
		CacheHitPreprocessed: raw.Stats.CacheHitPreprocessed,
		CacheMiss:            raw.Stats.CacheMiss,
		CacheHitRate:         raw.Stats.CacheHitRate,
		ErrorsCompiling:      raw.Stats.ErrorsCompiling,
		FilesInCache:         raw.Stats.FilesInCache,
		CacheSizeKibibyte:    raw.Stats.CacheSizeKibibyte,
	}, nil
}

// InvocationRunStats holds the runtime data captured around a single ccache-wrapped command run.
type InvocationRunStats struct {
	InvocationDate time.Time
	InvocationID   string
	Duration       time.Duration
	Command        string
	FullCommand    string
	Success        bool
	Error          error
	CcacheStats    CcacheStats
}

// NewInvocation assembles an Invocation from run stats, auth config, and system metadata.
func NewInvocation(runStats InvocationRunStats, authMetadata common.CacheAuthConfig, commonMetadata common.CacheConfigMetadata) *Invocation {
	errorStr := ""
	if runStats.Error != nil {
		errorStr = runStats.Error.Error()
	}

	return &Invocation{
		InvocationID:         runStats.InvocationID,
		InvocationDate:       runStats.InvocationDate,
		BitriseOrgSlug:       authMetadata.WorkspaceID,
		BitriseAppSlug:       commonMetadata.BitriseAppID,
		BitriseBuildSlug:     commonMetadata.BitriseBuildID,
		BitriseStepID:        commonMetadata.BitriseStepExecutionID,
		Hostname:             commonMetadata.HostMetadata.Hostname,
		Username:             commonMetadata.HostMetadata.Username,
		CommitHash:           commonMetadata.GitMetadata.CommitHash,
		Branch:               commonMetadata.GitMetadata.Branch,
		RepositoryURL:        commonMetadata.GitMetadata.RepoURL,
		CommitEmail:          commonMetadata.GitMetadata.CommitEmail,
		Command:              runStats.Command,
		FullCommand:          runStats.FullCommand,
		DurationMs:           runStats.Duration.Milliseconds(),
		Success:              runStats.Success,
		Error:                errorStr,
		WorkflowName:         commonMetadata.BitriseWorkflowName,
		ProviderID:           commonMetadata.CIProvider,
		CLIVersion:           commonMetadata.CLIVersion,
		Envs:                 commonMetadata.RedactedEnvs,
		OS:                   commonMetadata.HostMetadata.OS,
		HwCPUCores:           commonMetadata.HostMetadata.CPUCores,
		HwMemSize:            commonMetadata.HostMetadata.MemSize,
		Datacenter:           commonMetadata.Datacenter,
		DefaultCharset:       commonMetadata.HostMetadata.DefaultCharset,
		Locale:               commonMetadata.HostMetadata.Locale,
		ExternalAppID:        commonMetadata.ExternalAppID,
		ExternalBuildID:      commonMetadata.ExternalBuildID,
		ExternalWorkflowName: commonMetadata.ExternalWorkflowName,
		CcacheStats:          runStats.CcacheStats,
	}
}

// PutInvocation sends an Invocation to the analytics backend via HTTP PUT.
func (c *Client) PutInvocation(inv Invocation) error {
	requestURL := fmt.Sprintf("%s/invocations/%s", c.baseURL, inv.InvocationID)
	c.logger.Debugf("Sending ccache invocation data to: %s", requestURL)

	payload, err := json.Marshal(inv)
	if err != nil {
		return fmt.Errorf("marshal invocation: %w", err)
	}

	req, err := retryablehttp.NewRequest(http.MethodPut, requestURL, payload)
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return unwrapError(resp)
	}

	return nil
}
