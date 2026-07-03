package multiplatform

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

func init() { //nolint:gochecknoinits
	common.RegisterMultiplatformReader(readMultiplatformAuthConfig)
}

func readMultiplatformAuthConfig() (common.CacheAuthConfig, error) {
	cfg, err := ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil {
		return common.CacheAuthConfig{}, err //nolint:wrapcheck // surfaced only as a fallback signal
	}

	return cfg.AuthConfig, nil
}
