package common

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
)

type CacheConfigMetadata struct {
	CIProvider   string
	RepoURL      string
	HostMetadata HostMetadata
	// BitriseCI specific
	BitriseAppID        string
	BitriseWorkflowName string
	BitriseBuildID      string
	Datacenter          string
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

type bitriseCISpecificMetadata struct {
	BitriseAppID        string
	BitriseWorkflowName string
	BitriseBuildID      string
	Datacenter          string
}

// HostMetadata contains metadata about the local environment. Only used for Bazel to
// generate bazelrc. For Gradle, it's done by the plugin dynamically.
type HostMetadata struct {
	OS             string
	CPUCores       int
	MemSize        int64
	Locale         string
	DefaultCharset string
}

func createCacheConfigMetadata(provider, repoURL string,
	bitriseCIMetadata bitriseCISpecificMetadata,
	hostMetadata HostMetadata,
) CacheConfigMetadata {
	return CacheConfigMetadata{
		CIProvider: provider,
		RepoURL:    repoURL,
		// BitriseCI specific
		BitriseAppID:        bitriseCIMetadata.BitriseAppID,
		BitriseWorkflowName: bitriseCIMetadata.BitriseWorkflowName,
		BitriseBuildID:      bitriseCIMetadata.BitriseBuildID,
		Datacenter:          bitriseCIMetadata.Datacenter,
		// Only used for Bazel and only on CI
		HostMetadata: hostMetadata,
	}
}

// NewCacheConfigMetadata creates a new CacheConfigMetadata instance based on the environment variables.
func NewCacheConfigMetadata(envProvider EnvProviderFunc, commandFunc CommandFunc, logger log.Logger) CacheConfigMetadata {
	hostMetadata := generateHostMetadata(envProvider, commandFunc, logger)

	provider := detectCIProvider(envProvider)
	switch provider {
	case CIProviderBitrise:
		return createCacheConfigMetadata(provider, envProvider("GIT_REPOSITORY_URL"),
			bitriseCISpecificMetadata{
				BitriseAppID:        envProvider("BITRISE_APP_SLUG"),
				BitriseWorkflowName: envProvider("BITRISE_TRIGGERED_WORKFLOW_TITLE"),
				BitriseBuildID:      envProvider("BITRISE_BUILD_SLUG"),
				Datacenter:          envProvider(datacenterEnvKey),
			}, hostMetadata)
	case CIProviderCircleCI:
		return createCacheConfigMetadata(provider, envProvider("CIRCLE_REPOSITORY_URL"),
			bitriseCISpecificMetadata{}, hostMetadata)
	case CIProviderGitHubActions:
		repoURL := envProvider("GITHUB_SERVER_URL") + "/" + envProvider("GITHUB_REPOSITORY")

		return createCacheConfigMetadata(provider, repoURL,
			bitriseCISpecificMetadata{}, hostMetadata)
	}

	return CacheConfigMetadata{}
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
