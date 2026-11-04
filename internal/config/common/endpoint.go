package common

import (
	"slices"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/trustedurl"
)

const datacenterEnvKey = "BITRISE_DEN_VM_DATACENTER"

//nolint:gochecknoglobals
var (
	// nonRBEDCs are datacenters where RBE is not available
	nonRBEDCs = []string{
		consts.USEAST1,
	}
)

// SelectCacheEndpointURL - if endpointURL provided use that,
// otherwise select the best build cache endpoint automatically.
// The env override is used only when it is a grpcs URL on a Bitrise domain.
func SelectCacheEndpointURL(endpointURL string, envs map[string]string) string {
	if endpointURL == "" {
		endpointURL = trustedurl.FromEnv(envs, "BITRISE_BUILD_CACHE_ENDPOINT", "grpcs")
	}
	if len(endpointURL) > 0 {
		return endpointURL
	}

	return consts.BitriseAccelerate
}

// SelectRBEEndpointURL - if endpointURL provided use that,
// otherwise select the RBE endpoint from environment.
// The env override is used only when it is a grpcs URL or bare host:port on a Bitrise domain.
func SelectRBEEndpointURL(endpointURL string, envs map[string]string) string {
	if endpointURL == "" {
		endpointURL = trustedurl.FromEnv(envs, "BITRISE_RBE_ENDPOINT", "grpcs", "")
	}
	if len(endpointURL) > 0 {
		return endpointURL
	}

	bitriseDenVMDatacenter := envs[datacenterEnvKey]
	if slices.Contains(nonRBEDCs, bitriseDenVMDatacenter) {
		return ""
	}

	return consts.BitriseAccelerate
}

// ServicesURL maps a production services URL onto the domain named by BITRISE_BUILD_CACHE_SERVICES_DOMAIN.
// Only bitrise.dev is honoured; anything else keeps bitrise.io.
func ServicesURL(prodURL string, envs map[string]string) string {
	if !strings.EqualFold(strings.TrimSpace(envs[consts.EnvServicesDomain]), "bitrise.dev") {
		return prodURL
	}

	return strings.Replace(prodURL, ".services.bitrise.io", ".services.bitrise.dev", 1)
}

// XcodeAnalyticsURL is the Xcode analytics service URL for the services domain in envs.
func XcodeAnalyticsURL(envs map[string]string) string {
	return ServicesURL(consts.XcodeAnalyticsServiceEndpoint, envs)
}

// MultiplatformAnalyticsURL is the multiplatform analytics service URL for the services domain in envs.
func MultiplatformAnalyticsURL(envs map[string]string) string {
	return ServicesURL(consts.MultiplatformAnalyticsServiceEndpoint, envs)
}

// SelectWebsiteBaseURL returns the website base URL from the environment when it is an
// https URL on a Bitrise domain, else production. The token is sent there.
func SelectWebsiteBaseURL(envs map[string]string) string {
	if baseURL := trustedurl.FromEnv(envs, consts.EnvWebsiteBaseURL, "https"); baseURL != "" {
		return strings.TrimRight(baseURL, "/")
	}

	return consts.BitriseWebsiteBaseURL
}
