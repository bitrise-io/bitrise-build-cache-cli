package common

import (
	"runtime"
	"strconv"
	"strings"

	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	"os/user"
)

type CacheConfigMetadata struct {
	CIProvider   string
	CLIVersion   string
	HostMetadata HostMetadata
	GitMetadata  GitMetadata
	// BitriseCI specific
	BitriseAppID           string
	BitriseWorkflowName    string
	BitriseBuildID         string
	BitriseStepExecutionID string
	Datacenter             string
}

const (
	// CIProviderBitrise ...
	CIProviderBitrise = "bitrise"
	// CIProviderCircleCI ...
	CIProviderCircleCI = "circle-ci"
	// CIProviderGitHubActions ...
	CIProviderGitHubActions = "github-actions"

	// --- not used yet ---
	// CIProviderJenkins ...
	// CIProviderJenkins = "jenkins"
	// CIProviderUnknown ...
	// CIProviderUnknown = "other"
)

// EnvProviderFunc is a function which returns the value of an environment variable.
// It's compatible with os.Getenv - os.Getenv can be passed as an EnvProviderFunc.
type EnvProviderFunc func(string) string
type CommandFunc func(string, ...string) (string, error)

func detectCIProvider(envProvider EnvProviderFunc) string {
	if envProvider("BITRISE_IO") != "" {
		// https://devcenter.bitrise.io/en/references/available-environment-variables.html
		return CIProviderBitrise
	}
	if envProvider("CIRCLECI") != "" {
		// https://circleci.com/docs/variables/#built-in-environment-variables
		return CIProviderCircleCI
	}
	if envProvider("GITHUB_ACTIONS") != "" {
		// https://docs.github.com/en/actions/learn-github-actions/variables#default-environment-variables
		return CIProviderGitHubActions
	}

	return ""
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
	RepoURL     string
	CommitHash  string
	Branch      string
	CommitEmail string
}

// NewMetadata creates a new CacheConfigMetadata instance based on the environment variables.
func NewMetadata(envProvider EnvProviderFunc, commandFunc CommandFunc, logger log.Logger) CacheConfigMetadata {
	hostMetadata := generateHostMetadata(envProvider, commandFunc, logger)
	git := generateGitMetadata(logger, commandFunc, envProvider)
	cliVersion := envProvider("BITRISE_BUILD_CACHE_CLI_VERSION")
	provider := detectCIProvider(envProvider)
	if provider == CIProviderBitrise {
		return CacheConfigMetadata{
			CIProvider:             provider,
			CLIVersion:             cliVersion,
			GitMetadata:            git,
			HostMetadata:           hostMetadata,
			BitriseAppID:           envProvider("BITRISE_APP_SLUG"),
			BitriseWorkflowName:    envProvider("BITRISE_TRIGGERED_WORKFLOW_TITLE"),
			BitriseBuildID:         envProvider("BITRISE_BUILD_SLUG"),
			BitriseStepExecutionID: envProvider("BITRISE_STEP_EXECUTION_ID"),
			Datacenter:             envProvider(datacenterEnvKey),
		}
	}

	return CacheConfigMetadata{
		CIProvider:   provider,
		CLIVersion:   cliVersion,
		GitMetadata:  git,
		HostMetadata: hostMetadata,
	}
}

func generateGitMetadata(logger log.Logger, commandFunc CommandFunc, envProvider EnvProviderFunc) GitMetadata {
	gitMetadata := GitMetadata{}

	// Repo URL
	repoURL, err := commandFunc("git", "config", "--get", "remote.origin.url")
	if err != nil {
		logger.Debugf("Error in get git repo URL: %v", err)
		repoURL = envProvider("GIT_REPOSITORY_URL")
	}
	gitMetadata.RepoURL = strings.TrimSpace(repoURL)

	// Commit hash
	commitHash, err := commandFunc("git", "rev-parse", "HEAD")
	if err != nil {
		logger.Debugf("Error in get git commit hash: %v", err)
		commitHash = envProvider("GIT_CLONE_COMMIT_HASH")
	}
	gitMetadata.CommitHash = strings.TrimSpace(commitHash)

	// Branch
	branch, err := commandFunc("git", "branch", "--show-current")
	if err != nil {
		logger.Debugf("Error in get git branch: %v", err)
		branch = envProvider("BITRISE_GIT_BRANCH")
	}
	gitMetadata.Branch = strings.TrimSpace(branch)

	// Commit email
	commitEmail, err := commandFunc("git", "show", "-s", "--format=%ae", gitMetadata.CommitHash)
	if err != nil {
		logger.Debugf("Error in get git commit email: %v", err)
		commitEmail = envProvider("GIT_CLONE_COMMIT_AUTHOR_EMAIL")
	}
	gitMetadata.CommitEmail = strings.TrimSpace(commitEmail)

	return gitMetadata
}

// nolint: funlen, nestif
func generateHostMetadata(envProvider EnvProviderFunc, commandFunc CommandFunc, logger log.Logger) HostMetadata {
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
	localeRaw := getLocale(envProvider)
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

	// Username
	u, err := user.Current()
	if err != nil {
		logger.Errorf("Error in get username: %v", err)
	}
	metadata.Username = strings.TrimSpace(u.Username)

	return metadata
}

func getLocale(envProvider EnvProviderFunc) string {
	// Check various environment variables for locale information
	lang := envProvider("LANG")
	lcAll := envProvider("LC_ALL")
	if lcAll != "" {
		return lcAll
	}
	if lang != "" {
		return lang
	}

	return ""
}
