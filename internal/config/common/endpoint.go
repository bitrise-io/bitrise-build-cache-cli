package common

import (
	"slices"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
)

const datacenterEnvKey = "BITRISE_DEN_VM_DATACENTER"

//nolint:gochecknoglobals
var (
	cacheDCs = []string{
		consts.ATL1,
		consts.LAS1,
		consts.IAD1,
		consts.ORD1,
		consts.USEAST1,
	}
	rbeDCS = []string{
		consts.IAD1,
		consts.ORD1,
		consts.USEAST1,
	}
)

// SelectCacheEndpointURL - if endpointURL provided use that,
// otherwise select the best build cache endpoint automatically
func SelectCacheEndpointURL(endpointURL string, envProvider func(string) string) string {
	if endpointURL == "" {
		endpointURL = envProvider("BITRISE_BUILD_CACHE_ENDPOINT")
	}
	if len(endpointURL) > 0 {
		return endpointURL
	}

	bitriseDenVMDatacenter := envProvider(datacenterEnvKey)
	if slices.Contains(cacheDCs, bitriseDenVMDatacenter) {
		return consts.CacheInternalEndpointURLUnified
	}

	return consts.EndpointURLDefault
}

// SelectRBEEndpointURL - if endpointURL provided use that,
// otherwise select the RBE endpoint from environment
func SelectRBEEndpointURL(endpointURL string, envProvider func(string) string) string {
	if endpointURL == "" {
		endpointURL = envProvider("BITRISE_RBE_ENDPOINT")
	}
	if len(endpointURL) > 0 {
		return endpointURL
	}

	bitriseDenVMDatacenter := envProvider(datacenterEnvKey)
	if slices.Contains(rbeDCS, bitriseDenVMDatacenter) {
		return consts.RBEInternalEndpointURLUnified
	}

	return ""
}
