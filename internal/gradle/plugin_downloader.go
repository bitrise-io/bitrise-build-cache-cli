package gradle

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	errFmtPluginDownload = "failed to download file: %s"
)

type PluginDownloader struct {
	logger log.Logger
}

func (downloader PluginDownloader) Download(file PluginFile) (string, error) {
	artifactRepositories := []string{
		"https://plugins.gradle.org/m2/",
		"https://repo.maven.apache.org/maven2",
		"https://repo1.maven.org/maven2",
	}

	for _, artifactRepository := range artifactRepositories {
		err := PluginFileDownloader{
			file:          file,
			repositoryURL: artifactRepository,
			logger:        downloader.logger,
		}.Download()

		if err != nil {
			downloader.logger.Errorf("downloading %s from %s: %w", file.name(), artifactRepository, err)

			continue
		}

		return artifactRepository, nil
	}

	return "", fmt.Errorf(errFmtPluginDownload, file.name())
}
