package gradle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
)

const (
	bitriseGradlePluginGroup = "io.bitrise.gradle"
)

type PluginFile struct {
	groupID    string
	id         string
	version    string
	classifier string
	extension  string
}

func (gf *PluginFile) name() string {
	classifierPart := ""
	if gf.classifier != "" {
		classifierPart = "-" + gf.classifier
	}

	return fmt.Sprintf(
		"%s-%s%s.%s",
		gf.id,
		gf.version,
		classifierPart,
		gf.extension,
	)
}

func (gf *PluginFile) path() string {
	groupPath := strings.ReplaceAll(gf.groupID, ".", "/")

	return fmt.Sprintf(
		"%s/%s/%s",
		groupPath,
		gf.id,
		gf.version,
	)
}

func (gf *PluginFile) dir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("PWD")
	}

	return filepath.Join(home, ".m2", "repository", gf.path())
}

func (gf *PluginFile) key() string {
	return fmt.Sprintf(
		"%s:%s-test",
		gf.groupID,
		gf.name(),
	)
}

type BitriseGradlePlugin struct {
	id      string
	version string
}

func (plugin BitriseGradlePlugin) files() []PluginFile {
	return []PluginFile{
		{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, extension: "jar"},
		{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, extension: "module"},
		{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, extension: "pom"},
	}
}

func Plugins() []BitriseGradlePlugin {
	return []BitriseGradlePlugin{
		PluginCommon(),
		PluginAnalytics(),
		PluginCache(),
		PluginTestDistro(),
	}
}

func PluginCommon() BitriseGradlePlugin {
	return BitriseGradlePlugin{
		id:      "common",
		version: consts.GradleCommonPluginDepVersion,
	}
}

func PluginAnalytics() BitriseGradlePlugin {
	return BitriseGradlePlugin{
		id:      "gradle-analytics",
		version: consts.GradleAnalyticsPluginDepVersion,
	}
}

func PluginCache() BitriseGradlePlugin {
	return BitriseGradlePlugin{
		id:      "remote-cache",
		version: consts.GradleRemoteBuildCachePluginDepVersion,
	}
}

func PluginTestDistro() BitriseGradlePlugin {
	return BitriseGradlePlugin{
		id:      "test-distribution",
		version: consts.GradleTestDistributionPluginDepVersion,
	}
}
