package multiplatform

import (
	"fmt"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

// Invocation is the analytics payload for a run command, sent for every execution.
type Invocation struct {
	InvocationID         string            `json:"invocationId"`
	InvocationDate       time.Time         `json:"invocationDate"`
	BitriseWorkspaceSlug string            `json:"bitriseWorkspaceSlug"`
	BitriseAppSlug       string            `json:"bitriseAppSlug"`
	BitriseBuildSlug     string            `json:"bitriseBuildSlug"`
	BitriseStepID        string            `json:"bitriseStepId"`
	Hostname             string            `json:"hostname"`
	Username             string            `json:"username"`
	CommitHash           string            `json:"commitHash"`
	Branch               string            `json:"branch"`
	RepositoryURL        string            `json:"repositoryUrl"`
	CommitEmail          string            `json:"commitEmail"`
	Command              string            `json:"command"`
	FullCommand          string            `json:"fullCommand"`
	DurationMs           int64             `json:"durationMs"`
	Success              bool              `json:"success"`
	Error                string            `json:"error"`
	WorkflowName         string            `json:"workflowName"`
	ProviderID           string            `json:"providerId"`
	CLIVersion           string            `json:"cliVersion"`
	Envs                 map[string]string `json:"envs"`
	OS                   string            `json:"os"`
	HwCPUCores           int               `json:"hwCpuCores"`
	HwMemSize            int64             `json:"hwMemSize"`
	Datacenter           string            `json:"datacenter"`
	DefaultCharset       string            `json:"defaultCharset"`
	Locale               string            `json:"locale"`
	ExternalAppID        string            `json:"externalAppId,omitempty"`
	ExternalBuildID      string            `json:"externalBuildId,omitempty"`
	ExternalWorkflowName string            `json:"externalWorkflowName,omitempty"`
	BuildTool            string            `json:"buildTool"`
	Wrapper              string            `json:"wrapper,omitempty"`
}

// InvocationRelation records a parent→child relationship between two invocations.
type InvocationRelation struct {
	ParentInvocationID string    `json:"parentInvocationId"`
	ChildInvocationID  string    `json:"childInvocationId"`
	InvocationDate     time.Time `json:"invocationDate"`
	BuildTool          string    `json:"buildTool"`
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
	BuildTool      string
	Wrapper        string
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
		BitriseWorkspaceSlug: authMetadata.WorkspaceID,
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
		BuildTool:            runStats.BuildTool,
		Wrapper:              runStats.Wrapper,
	}
}

// PutInvocation sends an Invocation to the analytics backend via HTTP PUT.
func (c *Client) PutInvocation(inv Invocation) error {
	return c.Put(fmt.Sprintf("/v1/invocations/%s", inv.InvocationID), inv)
}

// PutInvocationRelation registers a parent→child invocation relationship with the analytics backend.
func (c *Client) PutInvocationRelation(rel InvocationRelation) error {
	return c.Put(fmt.Sprintf("/v1/invocations/%s/children/%s", rel.ParentInvocationID, rel.ChildInvocationID), rel)
}
