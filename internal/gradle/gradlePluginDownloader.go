package gradle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	errFmtPluginDownload = "failed to download plugin: %w"
)

type GradlePluginDownloader struct {
	groupID         string
	artifactID      string
	artifactVersion string
}

func (downloader GradlePluginDownloader) Download() (map[string]bool, error) {
	artifactRepositories := []string{
		"https://us-maven.pkg.dev/ip-build-cache-prod/build-cache-maven",
		"https://plugins.gradle.org/m2/",
		"https://repo.maven.apache.org/maven2",
		"https://repo1.maven.org/maven2",
	}

	files := []GradlePluginFile{
		{extension: "jar"},
		{extension: "module"},
		{extension: "pom"},
		//{classifier: "javadoc", extension: "jar"},
		//{classifier: "sources", extension: "jar"},
	}

	var errs []error
	sources := make(map[string]bool)

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5) // Limit parallelization
	for _, file := range files {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(file GradlePluginFile) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release a slot in the semaphore

			path, err := downloader.downloadPath(file)
			if err != nil {
				errs = append(errs, err)

				return
			}

			for _, artifactRepository := range artifactRepositories {
				err = GradlePluginFileDownloader{
					fileName:      downloader.filePath(file),
					repositoryURL: artifactRepository,
					downloadPath:  path,
				}.Download()

				if err == nil {
					sources[artifactRepository] = true
					break
				}
			}

			if err != nil {
				errs = append(errs, err)

				return
			}
		}(file)
	}

	wg.Wait()

	if len(errs) > 0 {
		return sources, fmt.Errorf(errFmtPluginDownload, errs[0])
	}

	return sources, nil
}

func (downloader GradlePluginDownloader) getMavenLocalRepoPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".m2", "repository"), nil
}

func (downloader GradlePluginDownloader) downloadPath(file GradlePluginFile) (string, error) {
	mavenLocalPath, err := downloader.getMavenLocalRepoPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(mavenLocalPath, downloader.filePath(file)), nil
}

func (downloader GradlePluginDownloader) filePath(file GradlePluginFile) string {
	groupPath := strings.ReplaceAll(downloader.groupID, ".", "/")
	classifierPart := ""
	if file.classifier != "" {
		classifierPart = "-" + file.classifier
	}

	return fmt.Sprintf(
		"%s/%s/%s/%s-%s%s.%s",
		groupPath,
		downloader.artifactID,
		downloader.artifactVersion,
		downloader.artifactID,
		downloader.artifactVersion,
		classifierPart,
		file.extension,
	)
}
