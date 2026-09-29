package bazelcredhelper

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Build metadata used to be baked into ~/.bazelrc at activation time, which
// pinned every build on the machine to whichever one ran `activate`. Resolving
// it here follows the invocation instead, the same way x-repository-url does.
const (
	orgIDHeader      = "x-org-id"
	appIDHeader      = "x-app-id"
	ciProviderHeader = "x-ci-provider"
	workflowHeader   = "x-workflow-name"
	buildUserHeader  = "x-flare-builduser"
	cacheBuildHeader = "x-flare-build-id"
	besBuildIDHeader = "x-build-id"
)

// MetadataResolver returns the per-invocation metadata headers.
type MetadataResolver func() map[string]string

// NewMetadataResolver reads the build's own environment. Env only, no git and no
// subprocess: Bazel spawns the helper under a tight timeout, and the repo URL
// lookup already spends the one git call this path can afford.
func NewMetadataResolver(envs map[string]string) MetadataResolver {
	return func() map[string]string {
		headers := map[string]string{}
		put := func(key, value string) {
			if value != "" {
				headers[key] = value
			}
		}

		provider := configcommon.DetectCIProvider(envs, utils.DefaultOsProxy{})
		put(ciProviderHeader, provider)

		if provider == configcommon.CIProviderBitrise {
			put(appIDHeader, envs["BITRISE_APP_SLUG"])
			put(workflowHeader, envs["BITRISE_TRIGGERED_WORKFLOW_ID"])
			putBuildID(put, envs["BITRISE_BUILD_SLUG"])
		} else {
			appID, buildID, workflow := configcommon.DetectExternalIDs(provider, envs)
			put(appIDHeader, appID)
			put(workflowHeader, workflow)
			putBuildID(put, buildID)
		}

		buildUser := provider
		if buildUser == "" {
			buildUser, _ = live.Default(nil).ResolveUsername(envs)
		}
		put(buildUserHeader, buildUser)

		return headers
	}
}

// The cache and BES read the build id under different keys, and one helper
// response answers both endpoints.
func putBuildID(put func(string, string), buildID string) {
	put(cacheBuildHeader, buildID)
	put(besBuildIDHeader, buildID)
}
