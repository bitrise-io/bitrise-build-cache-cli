package common

import (
	"runtime/debug"

	"github.com/bitrise-io/go-utils/v2/log"
)

func GetCLIVersion(logger log.Logger) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		logger.Infof("Failed to read build info")
		return "unknown"
	}

	return bi.Main.Version
}
