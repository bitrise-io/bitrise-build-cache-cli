package analytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

type InvocationRunStats struct {
	InvocationDate time.Time
	InvocationID   string
	Duration       int64
	HitRate        float32
	Command        string
	FullCommand    string
	Success        bool
	Error          error
	XcodeVersion   string
}

func NewInvocation(runStats InvocationRunStats, authMetadata common.CacheAuthConfig, commonMetadata common.CacheConfigMetadata) *Invocation {
	errorStr := ""
	if runStats.Error != nil {
		errorStr = runStats.Error.Error()
	}

	return &Invocation{
		InvocationID:     runStats.InvocationID,
		InvocationDate:   runStats.InvocationDate,
		BitriseOrgSlug:   authMetadata.WorkspaceID,
		BitriseAppSlug:   commonMetadata.BitriseAppID,
		BitriseBuildSlug: commonMetadata.BitriseBuildID,
		BitriseStepID:    commonMetadata.BitriseStepExecutionID,
		Hostname:         commonMetadata.HostMetadata.Hostname,
		Username:         commonMetadata.HostMetadata.Username,
		CommitHash:       commonMetadata.GitMetadata.CommitHash,
		Branch:           commonMetadata.GitMetadata.Branch,
		RepositoryURL:    commonMetadata.GitMetadata.RepoURL,
		CommitEmail:      commonMetadata.GitMetadata.CommitEmail,
		Command:          runStats.Command,
		FullCommand:      runStats.FullCommand,
		DurationMs:       runStats.Duration,
		HitRate:          runStats.HitRate,
		Success:          runStats.Success,
		Error:            errorStr,
		XcodeVersion:     runStats.XcodeVersion,
		WorkflowName:     commonMetadata.BitriseWorkflowName,
		ProviderID:       commonMetadata.CIProvider,
		CLIVersion:       commonMetadata.CLIVersion,
		Envs:             commonMetadata.RedactedEnvs,
		OS:               commonMetadata.HostMetadata.OS,
		HwCPUCores:       commonMetadata.HostMetadata.CPUCores,
		HwMemSize:        commonMetadata.HostMetadata.MemSize,
		Datacenter:       commonMetadata.Datacenter,
		DefaultCharset:   commonMetadata.HostMetadata.DefaultCharset,
		Locale:           commonMetadata.HostMetadata.Locale,
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
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))

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
