package gradleconfig

import (
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/stringmerge"
	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	ErrFmtGradlePropertiesCheck = "check if gradle.properties exists at %s, error: %w"
	ErrFmtGradlePropertyWrite   = "write gradle.properties to %s, error: %w"
)

type GradlePropertiesUpdater struct {
	OsProxy OsProxy
}

func DefaultGradlePropertiesUpdater() GradlePropertiesUpdater {
	return GradlePropertiesUpdater{
		OsProxy: DefaultOsProxy(),
	}
}

func (updater GradlePropertiesUpdater) UpdateGradleProps(
	params ActivateForGradleParams,
	logger log.Logger,
	gradleHomePath string,
) error {
	logger.Infof("(i) Write ~/.gradle/gradle.properties")

	gradlePropertiesPath := filepath.Join(gradleHomePath, "gradle.properties")
	currentGradlePropsFileContent, isGradlePropsExists, err := updater.OsProxy.ReadFileIfExists(gradlePropertiesPath)
	if err != nil {
		return fmt.Errorf(ErrFmtGradlePropertiesCheck, gradlePropertiesPath, err)
	}
	logger.Debugf("isGradlePropsExists: %t", isGradlePropsExists)

	cachingLine := "org.gradle.caching=true"
	if !params.Cache.Enabled {
		cachingLine = "org.gradle.caching=false"
	}

	gradlePropertiesContent := stringmerge.ChangeContentInBlock(
		currentGradlePropsFileContent,
		"# [start] generated-by-bitrise-build-cache",
		"# [end] generated-by-bitrise-build-cache",
		cachingLine,
	)

	err = updater.OsProxy.WriteFile(gradlePropertiesPath, []byte(gradlePropertiesContent), 0755) //nolint:gosec,gomnd,mnd
	if err != nil {
		return fmt.Errorf(ErrFmtGradlePropertyWrite, gradlePropertiesPath, err)
	}

	return nil
}
