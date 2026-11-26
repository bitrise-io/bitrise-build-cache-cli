package xcelerate

import "strconv"

// EnvDetailsUpload turns the analyzer Report upload on regardless of Config.DetailsUploadEnabled.
const EnvDetailsUpload = "BITRISE_BUILD_CACHE_XCODE_DETAILS_UPLOAD"

// ResolveDetailsUpload is the one gate for the analyzer Report upload, shared by the wrapper and the proxy.
func ResolveDetailsUpload(config Config, envs map[string]string) bool {
	if config.DetailsUploadEnabled {
		return true
	}

	enabled, err := strconv.ParseBool(envs[EnvDetailsUpload])

	return err == nil && enabled
}
