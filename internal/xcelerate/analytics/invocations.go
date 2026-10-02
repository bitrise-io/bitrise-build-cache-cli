package analytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

// MetricsSource tags the origin of the HitRate on an Invocation row so
// downstream analytics can tell wrapper-counter rows apart from log-scraped
// enrichment rows. Keep in sync with xcactivitylog.Outcome.String().
//
// BE schema alignment pending: metricsSource vs enrichmentSource vs
// cacheMetricsSource. Confirm the field name with the BE owner before PR
// merge — strict-schema reject would fail every orphan PUT.
const (
	// MetricsSourceActivityLog — HitRate parsed from the sibling .xcactivitylog.
	MetricsSourceActivityLog = "activity_log"
	// MetricsSourceLogMissing — log did not appear within the enricher's wait window.
	MetricsSourceLogMissing = "log_missing"
	// MetricsSourceLogEmpty — log present but decompressed body is empty.
	MetricsSourceLogEmpty = "log_empty"
	// MetricsSourceLogUnparsed — log present with content but no CompilationCacheMetrics match.
	MetricsSourceLogUnparsed = "log_unparsed"
	// MetricsSourceLogReadError — log present but open/stat/read failed (EACCES,
	// EIO, EMFILE, EISDIR). Distinct from LogUnparsed: we never got to read the
	// body, so "no match" cannot be concluded.
	MetricsSourceLogReadError = "log_read_error"
)

type InvocationRunStats struct {
	InvocationDate   time.Time
	InvocationID     string
	Duration         int64
	HitRate          float32
	Command          string
	FullCommand      string
	Success          bool
	Error            error
	XcodeVersion     string
	XcodeBuildNumber string
	CacheBlobStats   *blobstats.Snapshot
	MetricsSource    string
}

func NewInvocation(runStats InvocationRunStats, authMetadata auth.Credential, commonMetadata common.CacheConfigMetadata) *Invocation {
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
		DurationMs:           runStats.Duration,
		HitRate:              runStats.HitRate,
		Success:              runStats.Success,
		Error:                errorStr,
		XcodeVersion:         runStats.XcodeVersion,
		ToolBuildNumber:      runStats.XcodeBuildNumber,
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
		BenchmarkPhase:       commonMetadata.BenchmarkPhase,
		CacheBlobStats:       runStats.CacheBlobStats,
		MetricsSource:        runStats.MetricsSource,
	}
}

func (c *Client) PutInvocation(inv Invocation) error {
	requestURL := fmt.Sprintf("%s/invocations/%s", c.baseURL, inv.InvocationID)
	c.logger.Debugf("Sending invocation data to: %s", requestURL)

	payload, err := json.Marshal(inv)
	if err != nil {
		return fmt.Errorf("failed to marshal invocation: %w", err)
	}
	c.logger.Debugf("Payload: %s", payload)

	req, err := retryablehttp.NewRequest(http.MethodPut, requestURL, payload)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.tokenSupplier()))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return unwrapError(resp)
	}

	return nil
}
