package common

import (
	"encoding/json"
	"maps"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type CacheConfigMetadata struct {
	CIProvider   string
	CLIVersion   string
	RedactedEnvs map[string]string
	HostMetadata HostMetadata
	GitMetadata  GitMetadata
	// BitriseCI specific
	BitriseAppID           string
	BitriseWorkflowName    string
	BitriseBuildID         string
	BitriseStepExecutionID string
	Datacenter             string
	// External CI identifiers (non-Bitrise CI providers)
	ExternalAppID        string
	ExternalBuildID      string
	ExternalWorkflowName string
	// BenchmarkPhase is the benchmark phase (baseline, warmup, etc.)
	BenchmarkPhase string
}

const (
	// CIProviderBitrise ...
	CIProviderBitrise = "bitrise"
	// CIProviderCircleCI ...
	CIProviderCircleCI = "circle-ci"
	// CIProviderGitHubActions ...
	CIProviderGitHubActions = "github-actions"
	// CIProviderGitLabCI ...
	CIProviderGitLabCI = "gitlab-ci"
)

type CommandFunc func(string, ...string) (string, error)

// isRemoteDevEnv reports whether the CLI is running inside a Bitrise Remote Dev Environment session.
// RDE VMs get their hostname set to `vm-standalone-{linux,macos}-<uuid>` on bootstrap; regular CI VMs use a different prefix.
func isRemoteDevEnv(osProxy utils.OsProxy) bool {
	name, err := osProxy.Hostname()
	if err != nil {
		return false
	}

	return strings.HasPrefix(name, "vm-standalone-")
}

// IsCI is DetectCIProvider != "" widened to Build Hub runners under an
// unrecognised CI, which still need CI behaviour such as the file credential store.
func IsCI(envs map[string]string, osProxy utils.OsProxy) bool {
	return DetectCIProvider(envs, osProxy) != "" || auth.OnBuildHub(envs)
}

// DetectCIProvider inspects environment variables to identify which CI provider
// (if any) the current build is running on. Returns "" when no CI is detected.
func DetectCIProvider(envs map[string]string, osProxy utils.OsProxy) string {
	// RDE sessions leak BITRISE_IO/CI via /etc/profile; treat them as local regardless of other markers.
	if isRemoteDevEnv(osProxy) {
		return ""
	}

	// Check other CI providers first, so that Build Hub builds
	// (which also set BITRISE_IO) detect the original CI provider.
	if envs["CIRCLECI"] != "" {
		// https://circleci.com/docs/variables/#built-in-environment-variables
		return CIProviderCircleCI
	}
	if envs["GITHUB_ACTIONS"] != "" {
		// https://docs.github.com/en/actions/learn-github-actions/variables#default-environment-variables
		return CIProviderGitHubActions
	}
	if envs["GITLAB_CI"] == "true" {
		// https://docs.gitlab.com/ci/variables/predefined_variables/
		return CIProviderGitLabCI
	}
	if envs["BITRISE_IO"] != "" && envs["BITRISE_BUILD_SLUG"] != "" {
		// https://devcenter.bitrise.io/en/references/available-environment-variables.html
		// Build Hub sets BITRISE_IO but not BITRISE_BUILD_SLUG
		return CIProviderBitrise
	}

	return ""
}

func detectExternalIDs(provider string, envs map[string]string) (string, string, string) {
	switch provider {
	case CIProviderCircleCI:
		return envs["CIRCLE_PROJECT_REPONAME"], envs["CIRCLE_WORKFLOW_ID"], envs["CIRCLE_JOB"]
	case CIProviderGitHubActions:
		return envs["GITHUB_REPOSITORY"], envs["GITHUB_RUN_ID"], envs["GITHUB_JOB"]
	case CIProviderGitLabCI:
		return envs["CI_PROJECT_PATH"], envs["CI_PIPELINE_ID"], envs["CI_JOB_NAME"]
	default:
		return "", "", ""
	}
}

// HostMetadata contains metadata about the local environment. Only used for Bazel to
// generate bazelrc. For Gradle, it's done by the plugin dynamically.
type HostMetadata struct {
	OS             string
	CPUCores       int
	MemSize        int64
	Locale         string
	DefaultCharset string
	Hostname       string
	Username       string
}

type GitMetadata struct {
	RepoURL       string
	CommitHash    string
	Branch        string
	CommitEmail   string
	DefaultBranch string
}

// NewMetadata creates a new CacheConfigMetadata instance based on the environment variables.
func NewMetadata(envs map[string]string, username string, commandFunc CommandFunc, osProxy utils.OsProxy, logger log.Logger) CacheConfigMetadata {
	hostMetadata := generateHostMetadata(envs, username, commandFunc, logger)
	git := generateGitMetadata(logger, commandFunc, envs, osProxy)

	cliVersion := GetCLIVersion(logger)

	provider := DetectCIProvider(envs, osProxy)

	redactedEnvs := maps.Clone(envs)

	if provider == CIProviderBitrise {
		return CacheConfigMetadata{
			RedactedEnvs:           redactedEnvs,
			CIProvider:             provider,
			CLIVersion:             cliVersion,
			GitMetadata:            git,
			HostMetadata:           hostMetadata,
			BitriseAppID:           envs["BITRISE_APP_SLUG"],
			BitriseWorkflowName:    envs["BITRISE_TRIGGERED_WORKFLOW_ID"],
			BitriseBuildID:         envs["BITRISE_BUILD_SLUG"],
			BitriseStepExecutionID: envs["BITRISE_STEP_EXECUTION_ID"],
			Datacenter:             envs[datacenterEnvKey],
		}
	}

	externalAppID, externalBuildID, externalWorkflowName := detectExternalIDs(provider, envs)

	return CacheConfigMetadata{
		RedactedEnvs:         redactedEnvs,
		CIProvider:           provider,
		CLIVersion:           cliVersion,
		GitMetadata:          git,
		HostMetadata:         hostMetadata,
		ExternalAppID:        externalAppID,
		ExternalBuildID:      externalBuildID,
		ExternalWorkflowName: externalWorkflowName,
	}
}

func generateGitMetadata(logger log.Logger, commandFunc CommandFunc, envs map[string]string, osProxy utils.OsProxy) GitMetadata {
	gitMetadata := GitMetadata{}

	// Repo URL
	repoURL, err := ResolveRepoURL(commandFunc, envs)
	if err != nil {
		logger.Debugf("Error in get git repo URL: %v", err)
	}
	gitMetadata.RepoURL = repoURL

	// Commit hash
	commitHash, err := commandFunc("git", "rev-parse", "HEAD")
	if err != nil {
		logger.Debugf("Error in get git commit hash: %v", err)
		commitHash = envs["GIT_CLONE_COMMIT_HASH"]
	}
	gitMetadata.CommitHash = strings.TrimSpace(commitHash)

	// Branch
	branch := envs["BITRISE_GIT_BRANCH"]
	if branch == "" {
		branch, err = commandFunc("git", "branch", "--show-current")
		if err != nil {
			logger.Debugf("Error in get git branch: %v", err)
		}
	}
	if branch == "" {
		logger.Debugf("HEAD is probably detached, finding matching branches...")
		branch, err = commandFunc("sh", "-c", "git branch --contains HEAD --format='%(refname:short)' | grep -v HEAD")
		if err != nil {
			logger.Debugf("Error in get git branch (2nd attempt): %v", err)
		} else {
			matchingBranches := strings.Split(branch, "\n")
			if len(matchingBranches) == 1 {
				// Only if there's a single matching branch, otherwise leave empty to avoid confusion
				branch = matchingBranches[0]
			}
		}
	}
	branch = strings.TrimSpace(branch)
	if branch != "" {
		logger.Debugf("Detected git branch: %s", branch)
	}
	gitMetadata.Branch = branch

	// Default branch
	gitMetadata.DefaultBranch = resolveDefaultBranch(logger, commandFunc, envs, osProxy)

	// Commit email
	commitEmail, err := commandFunc("git", "show", "-s", "--format=%ae", gitMetadata.CommitHash)
	if err != nil {
		logger.Debugf("Error in get git commit email: %v", err)
		commitEmail = envs["GIT_CLONE_COMMIT_AUTHOR_EMAIL"]
	}
	gitMetadata.CommitEmail = strings.TrimSpace(commitEmail)

	return gitMetadata
}

// resolveDefaultBranch returns "" for Bitrise, where the API's own project record is authoritative,
// and for anything it cannot determine — remote HEAD is unset on the shallow single-branch clones
// most CI checkouts produce, so an empty result is an expected outcome rather than a failure.
func resolveDefaultBranch(logger log.Logger, commandFunc CommandFunc, envs map[string]string, osProxy utils.OsProxy) string {
	switch DetectCIProvider(envs, osProxy) {
	case CIProviderBitrise:
		return ""
	case CIProviderGitLabCI:
		if branch := envs["CI_DEFAULT_BRANCH"]; branch != "" {
			return branch
		}
	case CIProviderGitHubActions:
		if branch := defaultBranchFromGitHubEvent(logger, envs); branch != "" {
			return branch
		}
	}

	remoteHead, err := commandFunc("git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		logger.Debugf("Error in get default branch from remote HEAD: %v", err)

		return ""
	}

	return strings.TrimPrefix(strings.TrimSpace(remoteHead), "origin/")
}

// GitHub Actions publishes no environment variable for the default branch; the webhook payload it
// writes to disk is the only place a job can read it without an API call.
func defaultBranchFromGitHubEvent(logger log.Logger, envs map[string]string) string {
	eventPath := envs["GITHUB_EVENT_PATH"]
	if eventPath == "" {
		return ""
	}

	data, err := os.ReadFile(eventPath)
	if err != nil {
		logger.Debugf("Error in read GitHub event payload: %v", err)

		return ""
	}

	var event struct {
		Repository struct {
			DefaultBranch string `json:"default_branch"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		logger.Debugf("Error in parse GitHub event payload: %v", err)

		return ""
	}

	return event.Repository.DefaultBranch
}

// nolint: funlen, nestif
func generateHostMetadata(envs map[string]string, username string, commandFunc CommandFunc, logger log.Logger) HostMetadata {
	metadata := HostMetadata{}

	// OS
	detectedOS, err := commandFunc("uname", "-a")
	if err != nil {
		logger.Errorf("Error in get OS: %v", err)
	}
	logger.Debugf("OS: %s", detectedOS)
	metadata.OS = strings.TrimSpace(detectedOS)

	// CPU cores
	var ncpu int
	if runtime.GOOS == "darwin" {
		output, err := commandFunc("sysctl", "-n", "hw.ncpu")
		if err != nil {
			logger.Errorf("Error in get cpu cores: %v", err)
		} else if len(output) > 0 {
			ncpu, err = strconv.Atoi(strings.TrimSpace(output))
			if err != nil {
				logger.Errorf("Error in parse cpu cores: %v", err)
			}
		}
	} else {
		output, err := commandFunc("nproc")
		if err != nil {
			logger.Errorf("Error in get cpu cores: %v", err)
		} else if len(output) > 0 {
			ncpu, err = strconv.Atoi(strings.TrimSpace(output))
			if err != nil {
				logger.Errorf("Error in parse cpu cores: %v", err)
			}
		}
	}
	logger.Debugf("CPU cores: %d", ncpu)
	metadata.CPUCores = ncpu

	// RAM
	var memSize int64
	if runtime.GOOS == "darwin" {
		output, err := commandFunc("sysctl", "-n", "hw.memsize")
		if err != nil {
			logger.Errorf("Error in get mem size: %v", err)
		} else if len(output) > 0 {
			memSize, err = strconv.ParseInt(strings.TrimSpace(output), 10, 64)
			if err != nil {
				logger.Errorf("Error in parse mem size: %v", err)
			}
		}
	} else {
		output, err := commandFunc("sh", "-c",
			"grep MemTotal /proc/meminfo | tr -s ' ' | cut -d ' ' -f 2")
		if err != nil {
			logger.Errorf("Error in get mem size: %v", err)
		} else if len(output) > 0 {
			memSizeStr := strings.TrimSpace(output)
			memSize, err = strconv.ParseInt(memSizeStr, 10, 64)
			if err != nil {
				logger.Errorf("Error in parse mem size: %v", err)
			} else {
				memSize *= 1024 // Convert from KB to Bytes
			}
		}
	}
	logger.Debugf("Memory size: %d", memSize)
	metadata.MemSize = memSize

	// Locale
	localeRaw := getLocale(envs)
	localeComps := strings.Split(localeRaw, ".")
	if len(localeComps) == 2 {
		metadata.Locale = localeComps[0]
		metadata.DefaultCharset = localeComps[1]
	}
	logger.Debugf("Locale: %s", metadata.Locale)
	logger.Debugf("Default charset: %s", metadata.DefaultCharset)

	// Hostname
	hostname, err := os.Hostname()
	if err != nil {
		logger.Errorf("Error in get hostname: %v", err)
	}
	metadata.Hostname = strings.TrimSpace(hostname)

	metadata.Username = strings.TrimSpace(username)

	return metadata
}

func getLocale(envs map[string]string) string {
	// Check various environment variables for locale information
	lang := envs["LANG"]
	lcAll := envs["LC_ALL"]
	if lcAll != "" {
		return lcAll
	}
	if lang != "" {
		return lang
	}

	return ""
}
