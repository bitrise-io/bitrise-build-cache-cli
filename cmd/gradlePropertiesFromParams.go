package cmd

import (
	"fmt"
	"path/filepath"

	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/stringmerge"
	"github.com/bitrise-io/go-utils/v2/log"
)

type gradlePropertiesUpdater struct {
	readFileIfExists func(pth string) (string, bool, error)
	osProxy          gradleconfig.OsProxy
}

func defaultGradlePropertiesUpdater() gradlePropertiesUpdater {
	return gradlePropertiesUpdater{
		readFileIfExists: readFileIfExists,
		osProxy:          gradleconfig.DefaultOsProxy(),
	}
}

func (updater gradlePropertiesUpdater) updateGradleProps(
	params ActivateForGradleParams,
	logger log.Logger,
	gradleHomePath string,
) error {
	logger.Infof("(i) Write ~/.gradle/gradle.properties")

	gradlePropertiesPath := filepath.Join(gradleHomePath, "gradle.properties")
	currentGradlePropsFileContent, isGradlePropsExists, err := updater.readFileIfExists(gradlePropertiesPath)
	if err != nil {
		return fmt.Errorf(errFmtGradlePropertiesCheck, gradlePropertiesPath, err)
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

	err = updater.osProxy.WriteFile(gradlePropertiesPath, []byte(gradlePropertiesContent), 0755) //nolint:gosec,gomnd,mnd
	if err != nil {
		return fmt.Errorf(errFmtGradlePropertyWrite, gradlePropertiesPath, err)
	}

	return nil
}
