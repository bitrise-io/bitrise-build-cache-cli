package common

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
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

	bitriseDenVMDatacenter := envProvider("BITRISE_DEN_VM_DATACENTER")
	switch bitriseDenVMDatacenter {
	case "LAS1":
		return consts.EndpointURLLAS1
	case "ATL1":
		return consts.EndpointURLATL1
	case "IAD1":
		return consts.EndpointURLIAD1
	case "ORD1":
		return consts.EndpointURLORD1
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

	bitriseDenVMDatacenter := envProvider("BITRISE_DEN_VM_DATACENTER")
	switch bitriseDenVMDatacenter {
	case "IAD1":
		return consts.RBEEndpointURLIAD1
	case "ORD1":
		return consts.RBEEndpointURLORD1
	}

	return ""
}
