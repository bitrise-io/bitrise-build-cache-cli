package gradle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
)

const (
	bitriseGradlePluginGroup = "io.bitrise.gradle"
	WarnNoHome               = "Could not determine home directory, falling back to $PWD, error: %s"
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

func (gf *PluginFile) dirPath() string {
	groupPath := strings.ReplaceAll(gf.groupID, ".", "/")

	return fmt.Sprintf(
		"%s/%s/%s",
		groupPath,
		gf.id,
		gf.version,
	)
}

func (gf *PluginFile) absoluteDirPath(logger log.Logger) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("PWD")
		logger.Warnf(WarnNoHome, err)
	}

	return filepath.Join(home, ".m2", "repository", gf.dirPath())
}

func (gf *PluginFile) key() string {
	return fmt.Sprintf(
		"%s:%s",
		gf.groupID,
		gf.name(),
	)
}

type Plugin struct {
	id      string
	version string
}

func (plugin Plugin) files() []PluginFile {
	return []PluginFile{
		{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, extension: "jar"},
		{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, extension: "module"},
		{groupID: bitriseGradlePluginGroup, id: plugin.id, version: plugin.version, extension: "pom"},
	}
}

func Plugins() []Plugin {
	return []Plugin{
		PluginCommon(),
		PluginAnalytics(),
		PluginCache(),
		PluginTestDistro(),
	}
}

func PluginCommon() Plugin {
	return Plugin{
		id:      "common",
		version: consts.GradleCommonPluginDepVersion,
	}
}

func PluginAnalytics() Plugin {
	return Plugin{
		id:      "gradle-analytics",
		version: consts.GradleAnalyticsPluginDepVersion,
	}
}

func PluginCache() Plugin {
	return Plugin{
		id:      "remote-cache",
		version: consts.GradleRemoteBuildCachePluginDepVersion,
	}
}

func PluginTestDistro() Plugin {
	return Plugin{
		id:      "test-distribution",
		version: consts.GradleTestDistributionPluginDepVersion,
	}
}
