package analytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

// ParseCcacheStats parses the JSON output of `ccache --print-stats --format=json`.
// CacheHitRate is computed from direct and preprocessed hits over total attempts.
func ParseCcacheStats(data []byte) (CcacheStats, error) {
	var stats CcacheStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return CcacheStats{}, fmt.Errorf("parse ccache stats JSON: %w", err)
	}

	total := stats.DirectCacheHit + stats.PreprocessedCacheHit + stats.CacheMiss
	if total > 0 {
		stats.CacheHitRate = float64(stats.DirectCacheHit+stats.PreprocessedCacheHit) / float64(total)
	}

	return stats, nil
}

// InvocationRunStats holds the runtime data captured around a single run command execution.
type InvocationRunStats struct {
	InvocationDate time.Time
	InvocationID   string
	Duration       time.Duration
	Command        string
	FullCommand    string
	Success        bool
	Error          error
}

// NewInvocation assembles an Invocation from run stats, auth config, and system metadata.
// It captures command-level details and is sent regardless of ccache availability.
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
	}
}

// NewCcacheInvocation assembles a CcacheInvocation from a ccache stats snapshot and transfer byte counts.
// It references the parent Invocation via parentInvocationID and contains only ccache-specific data.
func NewCcacheInvocation(invocationID, parentInvocationID string, invocationDate time.Time, stats CcacheStats, downloadedBytes, uploadedBytes int64) *CcacheInvocation {
	return &CcacheInvocation{
		InvocationID:       invocationID,
		ParentInvocationID: parentInvocationID,
		InvocationDate:     invocationDate,
		CcacheStats:        stats,
		DownloadedBytes:    downloadedBytes,
		UploadedBytes:      uploadedBytes,
	}
}

// PutInvocationRelation registers a parent→child invocation relationship with the analytics backend.
// It is intended to be called at the start of a child invocation, before stats are available.
// NOTE: This is a placeholder — the backend endpoint is still under development.
func (c *Client) PutInvocationRelation(rel InvocationRelation) error {
	requestURL := fmt.Sprintf("%s/invocation-relations/%s", c.baseURL, rel.ChildInvocationID)
	c.logger.Debugf("Registering invocation relation: parent=%s child=%s", rel.ParentInvocationID, rel.ChildInvocationID)

	payload, err := json.Marshal(rel)
	if err != nil {
		return fmt.Errorf("marshal invocation relation: %w", err)
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

// PutInvocation sends an Invocation to the analytics backend via HTTP PUT.
// NOTE: This is a placeholder — the backend endpoint is still under development.
func (c *Client) PutInvocation(inv Invocation) error {
	requestURL := fmt.Sprintf("%s/invocations/%s", c.baseURL, inv.InvocationID)
	c.logger.Debugf("Sending run invocation data to: %s", requestURL)

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

// PutCcacheInvocation sends a CcacheInvocation to the analytics backend via HTTP PUT.
// NOTE: This is a placeholder — the backend endpoint is still under development.
func (c *Client) PutCcacheInvocation(inv CcacheInvocation) error {
	requestURL := fmt.Sprintf("%s/ccache-invocations/%s", c.baseURL, inv.InvocationID)
	c.logger.Debugf("Sending ccache invocation data to: %s", requestURL)

	payload, err := json.Marshal(inv)
	if err != nil {
		return fmt.Errorf("marshal ccache invocation: %w", err)
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
