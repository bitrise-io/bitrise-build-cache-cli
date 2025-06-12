package gradle

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
)

const (
	bitriseGradlePluginGroup = "io.bitrise.gradle"
)

type GradlePluginFile struct {
	groupID    string
	id         string
	version    string
	classifier string
	extension  string
}

func (gf *GradlePluginFile) name() string {
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

func (gf *GradlePluginFile) path() string {
	groupPath := strings.ReplaceAll(gf.groupID, ".", "/")

	return fmt.Sprintf(
		"%s/%s/%s/%s",
		groupPath,
		gf.id,
		gf.version,
		gf.name(),
	)
}

func (gf *GradlePluginFile) key() string {
	return fmt.Sprintf(
		"%s:%s",
		gf.groupID,
		gf.name(),
	)
}

type BitriseGradlePlugin struct {
	id      string
	version string
}

func (plugin BitriseGradlePlugin) files() []GradlePluginFile {
	return []GradlePluginFile{
		{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, extension: "jar"},
		{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, extension: "module"},
		{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, extension: "pom"},
		//{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, classifier: "javadoc", extension: "jar"},
		//{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, classifier: "sources", extension: "jar"},
	}
}

func GradlePlugins() []BitriseGradlePlugin {
	return []BitriseGradlePlugin{
		//GradlePluginCommon(),
		GradlePluginAnalytics(),
		GradlePluginCache(),
		//gradlePluginTestDistro(),
	}
}

func GradlePluginCommon() BitriseGradlePlugin {
	return BitriseGradlePlugin{
		id:      "common",
		version: consts.GradleCommonPluginDepVersion,
	}
}

func GradlePluginAnalytics() BitriseGradlePlugin {
	return BitriseGradlePlugin{
		id:      "gradle-analytics",
		version: consts.GradleAnalyticsPluginDepVersion,
	}
}

func GradlePluginCache() BitriseGradlePlugin {
	return BitriseGradlePlugin{
		id:      "remote-cache",
		version: consts.GradleRemoteBuildCachePluginDepVersion,
	}
}

func GradlePluginTestDistro() BitriseGradlePlugin {
	return BitriseGradlePlugin{
		id:      "test-distribution",
		version: consts.GradleTestDistributionPluginDepVersion,
	}
}
