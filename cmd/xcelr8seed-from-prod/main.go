// xcelr8seed-from-prod replays prod xcode invocations into the local
// xcode-analytics-service stack. Reads `_xcelr8/snapshots/list-xcode.json`
// (the presenter-shape snapshot pulled by `scripts/xcelr8_snapshot_prod.sh`),
// fills in the fields the presenter doesn't expose with deterministic
// synthetic values (commit email + hostname rotated over a small set so the
// new ACI-4908 / 4909 filters can split the data), and PUTs each into the
// local stack.
//
// Usage (defaults match the local hackathon stack):
//
//	go run ./cmd/xcelr8seed-from-prod \
//	    --input _xcelr8/snapshots/list-xcode.json \
//	    --base-url http://localhost:3000 \
//	    --org test-org
//
// Synthetic fields are clearly marked (e.g. commit emails end in
// `@example.com`); never PUT this output back to a prod service.
package main

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // not security-critical, just deterministic bucketing
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Synthetic users + macs. Rotated deterministically so re-runs stay stable.
var syntheticEmails = []string{
	"balazs@bitrise.io",
	"zsolt@bitrise.io",
	"ramil@bitrise.io",
	"peter@bitrise.io",
	"anna@bitrise.io",
}

var syntheticHostnames = []string{
	"MacBook-balazs.local",
	"MacBook-zsolt.local",
	"MacBook-ramil.local",
	"MacBook-peter.local",
	"MacBook-anna.local",
	"Studio-shared-1.local",
	"Studio-shared-2.local",
	"MacMini-CI-runner.local",
}

// Presenter-shape item — only the fields we actually use.
type presenterItem struct {
	InvocationID    string   `json:"invocationId"`
	Tool            string   `json:"tool"`
	ProjectSlug     string   `json:"projectSlug"`
	BuildSlug       string   `json:"buildSlug"`
	RepositoryURL   string   `json:"repositoryUrl"`
	CommitHash      string   `json:"commitHash"`
	WorkflowName   string   `json:"workflowName"`
	CIProvider      string   `json:"ciProvider"`
	Command         string   `json:"command"`
	ShortCommand    string   `json:"shortCommand"`
	StartedAt       string   `json:"startedAt"`
	StepExecutionID string   `json:"stepExecutionId"`
	Status          string   `json:"status"`
	ToolVersion     string   `json:"toolVersion"`
	ToolBuildNumber string   `json:"toolBuildNumber"`
	Duration        *float64 `json:"duration"`
	CacheHitRate    *float64 `json:"cacheHitRate"`
	BranchName      string   `json:"branchName"`
	BenchmarkPhase  string   `json:"benchmarkPhase"`
}

type presenterList struct {
	Items []presenterItem `json:"items"`
}

// Service Invocation — same shape as
// xcode-analytics-service `internal/model/Invocation`.
type serviceInvocation struct {
	InvocationID         string            `json:"invocationId"`
	InvocationDate       string            `json:"invocationDate"`
	CreatedAt            string            `json:"createdAt"`
	BitriseOrgSlug       string            `json:"bitriseOrgSlug"`
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
	HitRate              float32           `json:"hitRate"`
	Success              bool              `json:"success"`
	Error                string            `json:"error"`
	XcodeVersion         string            `json:"xcodeVersion"`
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
	ToolBuildNumber      string            `json:"toolBuildNumber"`
	ExternalAppID        string            `json:"externalAppId"`
	ExternalWorkflowName string            `json:"externalWorkflowName"`
	ExternalBuildID      string            `json:"externalBuildId"`
	BenchmarkPhase       string            `json:"benchmarkPhase"`
}

func main() {
	var (
		input       = flag.String("input", "_xcelr8/snapshots/list-xcode.json", "path to presenter-shape snapshot")
		baseURL     = flag.String("base-url", "http://localhost:3000", "xcode-analytics-service base URL")
		orgSlug     = flag.String("org", "test-org", "Bitrise workspace / org slug to write under (must match the local stack auth token)")
		toolFilter  = flag.String("tool", "xcode", "only PUT items where presenter `tool` equals this; xcode-analytics-service stores xcode invocations only")
		dryRun      = flag.Bool("dry-run", false, "render PUT bodies to stdout, do not call the service")
	)
	flag.Parse()

	data, err := os.ReadFile(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", *input, err)
		os.Exit(1)
	}

	var lf presenterList
	if err := json.Unmarshal(data, &lf); err != nil {
		fmt.Fprintf(os.Stderr, "decode %s: %v\n", *input, err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	var put, skipped, failed int

	for i, p := range lf.Items {
		if *toolFilter != "" && p.Tool != *toolFilter {
			skipped++

			continue
		}

		inv := mapItem(p, *orgSlug)

		if *dryRun {
			body, _ := json.MarshalIndent(inv, "", "  ")
			fmt.Printf("--- PUT /invocations/%s ---\n%s\n", inv.InvocationID, body)
			put++

			continue
		}

		body, err := json.Marshal(inv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%d] marshal: %v\n", i, err)
			failed++

			continue
		}

		req, err := http.NewRequest(http.MethodPut, *baseURL+"/invocations/"+inv.InvocationID, bytes.NewReader(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%d] new req: %v\n", i, err)
			failed++

			continue
		}
		req.Header.Set("Authorization", "Bearer "+*orgSlug)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%d] do: %v\n", i, err)
			failed++

			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if failed < 3 {
				fmt.Fprintf(os.Stderr, "[%d] %s — HTTP %d: %s\n", i, inv.InvocationID, resp.StatusCode, clip(string(respBody), 200))
			}
			failed++

			continue
		}

		put++
	}

	fmt.Printf("done — put=%d skipped=%d failed=%d\n", put, skipped, failed)

	if failed > 0 {
		os.Exit(1)
	}
}

func mapItem(p presenterItem, orgSlug string) serviceInvocation {
	idx := bucket(p.InvocationID)

	startedAt := p.StartedAt
	if startedAt == "" {
		startedAt = time.Now().UTC().Format(time.RFC3339)
	}

	durationMs := int64(0)
	if p.Duration != nil {
		// Presenter `duration` unit is opaque (per ACI-4914 contract). Most
		// xcode rows in the snapshot are sub-1000 → assume seconds, convert
		// to ms. Cheap heuristic; fine for hackathon fixtures.
		durationMs = int64(*p.Duration * 1000.0)
	}

	hitRate := float32(0)
	if p.CacheHitRate != nil {
		hitRate = float32(*p.CacheHitRate)
	}

	xcodeVersion := p.ToolVersion
	if xcodeVersion == "" {
		xcodeVersion = "16.0"
	}

	return serviceInvocation{
		InvocationID:     p.InvocationID,
		InvocationDate:   startedAt,
		CreatedAt:        startedAt,
		BitriseOrgSlug:   orgSlug,
		BitriseAppSlug:   firstNonEmpty(p.ProjectSlug, "synth-app-"+shortHash(p.InvocationID, 8)),
		BitriseBuildSlug: p.BuildSlug,
		BitriseStepID:    p.StepExecutionID,
		Hostname:         syntheticHostnames[idx%len(syntheticHostnames)], // synthesized — see ACI-4909
		Username:         "developer",
		CommitHash:       p.CommitHash,
		Branch:           p.BranchName,
		RepositoryURL:    p.RepositoryURL,
		CommitEmail:      syntheticEmails[idx%len(syntheticEmails)], // synthesized — see ACI-4908
		Command:          p.ShortCommand,
		FullCommand:      p.Command,
		DurationMs:       durationMs,
		HitRate:          hitRate,
		Success:          strings.EqualFold(p.Status, "success"),
		Error:            "",
		XcodeVersion:     xcodeVersion,
		WorkflowName:     p.WorkflowName,
		ProviderID:       normaliseProviderID(p.CIProvider),
		CLIVersion:       "synth",
		Envs:             map[string]string{},
		OS:               "macOS 14.0",
		HwCPUCores:       8,
		HwMemSize:        17179869184,
		Datacenter:       "local",
		DefaultCharset:   "UTF-8",
		Locale:           "en_US",
		ToolBuildNumber:  p.ToolBuildNumber,
		BenchmarkPhase:   p.BenchmarkPhase,
	}
}

// bucket maps an invocation ID to a stable integer index for synthetic
// rotation. Re-runs with the same input produce the same assignments.
func bucket(id string) int {
	if id == "" {
		return 0
	}

	h := sha1.Sum([]byte(id)) //nolint:gosec
	v := binary.BigEndian.Uint32(h[:4])

	return int(v)
}

// shortHash returns the first n hex chars of sha1(s) — used for synthetic
// fallback IDs when the presenter omitted a value.
func shortHash(s string, n int) string {
	h := sha1.Sum([]byte(s)) //nolint:gosec

	hex := fmt.Sprintf("%x", h)
	if len(hex) < n {
		return hex
	}

	return hex[:n]
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

// normaliseProviderID maps presenter ciProvider strings to the values the
// service stores in `provider_id`. Empty for unknown / local builds, matching
// what the existing CLI sends — keeps ACI-4910 (`providerId=unknown`) honest.
func normaliseProviderID(ciProvider string) string {
	switch strings.ToLower(ciProvider) {
	case "", "unknown":
		return ""
	default:
		return strings.ToLower(ciProvider)
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "…"
}
