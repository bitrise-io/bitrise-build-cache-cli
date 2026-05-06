// xcelr8seed-synthetic generates a realistic-looking, fully synthetic
// dataset of xcode invocations and PUTs them into a local
// xcode-analytics-service stack.
//
// No prod data, no PII. Use for hackathon demos when you want a known
// shape without snapshotting prod first.
//
// Defaults: 200 invocations spread across the last 30 days, 5 fake users,
// 8 hostnames, 5 projects, mix of local / bitrise / github CI providers,
// success-heavy with some failures, hit-rate distribution skewed toward
// hits.
//
// Usage:
//
//	go run ./cmd/xcelr8seed-synthetic --count 200 --org test-org
//	go run ./cmd/xcelr8seed-synthetic --count 1000 --seed 42
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"os"
	"time"
)

//nolint:gochecknoglobals
var (
	emails = []string{
		"balazs@bitrise.io",
		"zsolt@bitrise.io",
		"ramil@bitrise.io",
		"peter@bitrise.io",
		"anna@bitrise.io",
	}

	hostnames = []string{
		"MacBook-balazs.local",
		"MacBook-zsolt.local",
		"MacBook-ramil.local",
		"MacBook-peter.local",
		"MacBook-anna.local",
		"Studio-shared-1.local",
		"Studio-shared-2.local",
		"MacMini-CI-runner.local",
	}

	projects = []struct {
		Slug    string
		Title   string
		RepoURL string
	}{
		{"proj-aurora", "iOS-app-aurora", "git@github.com:example/aurora.git"},
		{"proj-borealis", "iOS-app-borealis", "git@github.com:example/borealis.git"},
		{"proj-comet", "iOS-app-comet", "git@github.com:example/comet.git"},
		{"proj-dawn", "iOS-app-dawn", "git@github.com:example/dawn.git"},
		{"proj-eclipse", "iOS-app-eclipse", "git@github.com:example/eclipse.git"},
	}

	workflows = []string{"primary", "deploy", "test", "release", "pr"}

	branches = []string{"main", "develop", "feature/onboarding", "feature/payments", "feature/search", "release/1.2", "fix/crash-on-launch"}

	commands = []string{
		"build -workspace App.xcworkspace -scheme App",
		"test -workspace App.xcworkspace -scheme App -destination platform=iOS Simulator",
		"archive -workspace App.xcworkspace -scheme App -configuration Release",
		"build-for-testing -workspace App.xcworkspace -scheme AppTests",
		"clean build -workspace App.xcworkspace -scheme App",
	}

	shortCommands = []string{"build", "test", "archive", "build-for-testing", "build"}

	// Provider distribution — sum should be 100.
	providerWeights = []struct {
		ID     string
		Weight int
	}{
		{"", 60},        // local
		{"bitrise", 25}, // bitrise CI
		{"github", 10},  // GitHub Actions
		{"gitlab", 5},   // GitLab CI
	}
)

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
		count      = flag.Int("count", 200, "number of invocations to generate")
		baseURL    = flag.String("base-url", "http://localhost:3000", "xcode-analytics-service base URL")
		orgSlug    = flag.String("org", "test-org", "Bitrise workspace / org slug")
		windowDays = flag.Int("window-days", 30, "spread invocationDate uniformly across the last N days")
		seed       = flag.Int64("seed", 1, "RNG seed for deterministic re-runs")
		dryRun     = flag.Bool("dry-run", false, "print PUT bodies to stdout, do not call the service")
	)
	flag.Parse()

	rng := mrand.New(mrand.NewSource(*seed)) //nolint:gosec
	client := &http.Client{Timeout: 10 * time.Second}

	now := time.Now().UTC()
	windowStart := now.Add(-time.Duration(*windowDays) * 24 * time.Hour)

	var put, failed int

	for i := range *count {
		inv := generate(rng, *orgSlug, windowStart, now)

		if *dryRun {
			body, err := json.MarshalIndent(inv, "", "  ")
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%d] dry-run marshal: %v\n", i, err)
				failed++

				continue
			}
			fmt.Fprintf(os.Stdout, "--- PUT /invocations/%s ---\n%s\n", inv.InvocationID, body)
			put++

			continue
		}

		body, err := json.Marshal(inv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%d] marshal: %v\n", i, err)
			failed++

			continue
		}

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, *baseURL+"/invocations/"+inv.InvocationID, bytes.NewReader(body))
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

	fmt.Fprintf(os.Stdout, "done — put=%d failed=%d (count=%d, seed=%d, window=%dd)\n", put, failed, *count, *seed, *windowDays)

	if failed > 0 {
		os.Exit(1)
	}
}

func generate(rng *mrand.Rand, orgSlug string, start, end time.Time) serviceInvocation {
	proj := projects[rng.Intn(len(projects))]
	cmdIdx := rng.Intn(len(commands))

	// Time uniformly in [start, end).
	dt := start.Add(time.Duration(rng.Int63n(int64(end.Sub(start)))))

	// Duration: log-normal-ish, 30s..6min, skewed toward 1–2 minutes.
	durSec := 30.0 + rng.NormFloat64()*45.0 + 60.0
	if durSec < 5 {
		durSec = 5
	}
	if durSec > 360 {
		durSec = 360
	}

	// Hit rate distribution: 30% miss, 30% mid, 40% high.
	var hit float32

	switch r := rng.Float64(); {
	case r < 0.30:
		hit = 0.0
	case r < 0.60:
		hit = float32(0.4 + rng.Float64()*0.3) // 0.4–0.7
	default:
		hit = float32(0.7 + rng.Float64()*0.25) // 0.7–0.95
	}

	success := rng.Float64() < 0.90 // 90% success

	// Synthetic UUID-ish ID (16 random bytes hex).
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		// Fallback to math/rand stream if crypto/rand unavailable.
		for i := range idBytes {
			idBytes[i] = byte(rng.Intn(256))
		}
	}
	id := hex.EncodeToString(idBytes)

	commitBytes := make([]byte, 20)
	for i := range commitBytes {
		commitBytes[i] = byte(rng.Intn(256))
	}

	return serviceInvocation{
		InvocationID:     id,
		InvocationDate:   dt.Format(time.RFC3339),
		CreatedAt:        dt.Format(time.RFC3339),
		BitriseOrgSlug:   orgSlug,
		BitriseAppSlug:   proj.Slug,
		BitriseBuildSlug: "build-" + hex.EncodeToString(idBytes[:4]),
		BitriseStepID:    "xcode-build",
		Hostname:         hostnames[rng.Intn(len(hostnames))],
		Username:         "developer",
		CommitHash:       hex.EncodeToString(commitBytes),
		Branch:           branches[rng.Intn(len(branches))],
		RepositoryURL:    proj.RepoURL,
		CommitEmail:      emails[rng.Intn(len(emails))],
		Command:          shortCommands[cmdIdx],
		FullCommand:      "xcodebuild " + commands[cmdIdx],
		DurationMs:       int64(durSec * 1000),
		HitRate:          hit,
		Success:          success,
		Error:            errorMsg(success, rng),
		XcodeVersion:     "16.0",
		WorkflowName:     workflows[rng.Intn(len(workflows))],
		ProviderID:       weightedProvider(rng),
		CLIVersion:       "synth",
		Envs:             map[string]string{},
		OS:               "macOS 14.0",
		HwCPUCores:       8,
		HwMemSize:        17179869184,
		Datacenter:       "synth",
		DefaultCharset:   "UTF-8",
		Locale:           "en_US",
		ToolBuildNumber:  "16A242d",
	}
}

func weightedProvider(rng *mrand.Rand) string {
	pick := rng.Intn(100)

	cum := 0
	for _, p := range providerWeights {
		cum += p.Weight
		if pick < cum {
			return p.ID
		}
	}

	return ""
}

func errorMsg(success bool, rng *mrand.Rand) string {
	if success {
		return ""
	}

	msgs := []string{
		"Build operation failed: linker command failed with exit code 1",
		"Test failure: APITests.testFetchUser failed",
		"Code signing error: no provisioning profile",
		"Compile error: cannot find type 'FooView' in scope",
		"Module compilation error",
	}

	return msgs[rng.Intn(len(msgs))]
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "…"
}
