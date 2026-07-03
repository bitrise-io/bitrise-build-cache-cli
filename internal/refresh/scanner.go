// Package refresh scans the per-tool config files on disk and nudges the user
// to re-activate a tool whose persisted config schema major-version is behind
// the CLI's current expectation.
package refresh

import (
	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle"
	xcelerateconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Scan returns one Sample per tool with a config present on disk.
// Tools without a config file are silently absent from the returned slice.
func Scan(home string) []toolconfig.Sample {
	scanners := []func(string) (toolconfig.Sample, bool){
		scanGradle,
		scanBazel,
		scanXcelerate,
		scanCcache,
	}

	var out []toolconfig.Sample
	for _, scan := range scanners {
		if s, ok := scan(home); ok {
			out = append(out, s)
		}
	}

	return out
}

func scanGradle(home string) (toolconfig.Sample, bool) {
	s, ok, err := gradleconfig.ReadSidecar(home)
	if err != nil || !ok {
		return toolconfig.Sample{}, false
	}

	return toolconfig.Sample{
		Tool:          toolconfig.Gradle,
		ConfigVersion: s.ConfigVersion,
		WrittenAt:     s.WrittenAt,
		ConfigPath:    gradleconfig.SidecarFilePath(home),
	}, true
}

func scanBazel(home string) (toolconfig.Sample, bool) {
	s, ok, err := bazelconfig.ReadSidecar(home)
	if err != nil || !ok {
		return toolconfig.Sample{}, false
	}

	return toolconfig.Sample{
		Tool:          toolconfig.Bazel,
		ConfigVersion: s.ConfigVersion,
		WrittenAt:     s.WrittenAt,
		ConfigPath:    bazelconfig.SidecarFilePath(home),
	}, true
}

func scanXcelerate(home string) (toolconfig.Sample, bool) {
	osProxy := utils.DefaultOsProxy{}
	cfg, err := xcelerateconfig.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
	if err != nil {
		return toolconfig.Sample{}, false
	}

	return toolconfig.Sample{
		Tool:          toolconfig.Xcelerate,
		ConfigVersion: cfg.ConfigVersion,
		WrittenAt:     cfg.WrittenAt,
		ConfigPath:    xcelerateconfig.PathFor(osProxy, "config.json"),
	}, true
}

func scanCcache(home string) (toolconfig.Sample, bool) {
	osProxy := utils.DefaultOsProxy{}
	cfg, err := ccacheconfig.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
	if err != nil {
		return toolconfig.Sample{}, false
	}

	return toolconfig.Sample{
		Tool:          toolconfig.Ccache,
		ConfigVersion: cfg.ConfigVersion,
		WrittenAt:     cfg.WrittenAt,
		ConfigPath:    ccacheconfig.PathFor(osProxy, "config.json"),
	}, true
}
