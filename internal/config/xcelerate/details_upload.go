package xcelerate

import "strconv"

// EnvDetailsUpload, when set to a boolean, overrides Config.DetailsUploadEnabled in either direction.
const EnvDetailsUpload = "BITRISE_BUILD_CACHE_XCODE_DETAILS_UPLOAD"

// ResolveDetailsUpload reports whether the analyzer Report upload is on.
func ResolveDetailsUpload(config Config, envs map[string]string) bool {
	if enabled, err := strconv.ParseBool(envs[EnvDetailsUpload]); err == nil {
		return enabled
	}

	return config.DetailsUploadEnabled
}
