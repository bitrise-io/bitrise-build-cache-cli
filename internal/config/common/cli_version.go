package common

import (
	"runtime/debug"
	"slices"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
)

func GetCLIVersion(logger log.Logger) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		logger.Infof("Failed to read build info")

		return "unknown"
	}

	// Find the bitrise-build-cache-cli module in the build info.
	// If ran from source, it will be the main module.
	// If ran from a step, it will be a dependency module.
	modules := []*debug.Module{&bi.Main}
	modules = append(modules, bi.Deps...)
	idx := slices.IndexFunc(modules, func(c *debug.Module) bool { return strings.Contains(c.Path, "bitrise-build-cache-cli") })
	if idx == -1 || idx >= len(modules) {
		logger.Infof("Failed to find bitrise-build-cache-cli module in build info")

		return "unknown"
	}

	return modules[idx].Version
}
