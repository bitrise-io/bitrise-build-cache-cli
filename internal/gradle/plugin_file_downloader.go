package gradle

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
)

const (
	errFmtDownloading = "error while downloading: %w"
	errFmtBadStatus   = "bad status: %s"
	errFmtCreateFile  = "error while creating file: %w"
	errFmtWritingFile = "error while writing file: %w"
)

const (
	maxHTTPClientRetries = 3
)

type PluginFileDownloader struct {
	file          PluginFile
	repositoryURL string
	logger        log.Logger
}

func (downloader PluginFileDownloader) Download() error {
	httpClient := retryhttp.NewClient(downloader.logger)
	httpClient.RetryMax = maxHTTPClientRetries

	resp, err := httpClient.Get(downloader.repositoryURL + "/" + downloader.file.dirPath() + "/" + downloader.file.name())
	if err != nil {
		return fmt.Errorf(errFmtDownloading, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(errFmtBadStatus, resp.Status)
	}

	// Create the file
	if err := os.MkdirAll(downloader.file.absoluteDirPath(downloader.logger), os.ModePerm); err != nil {
		return fmt.Errorf(errFmtCreateFile, err)
	}
	out, err := os.Create(filepath.Join(downloader.file.absoluteDirPath(downloader.logger), downloader.file.name()))
	if err != nil {
		return fmt.Errorf(errFmtCreateFile, err)
	}
	defer out.Close()

	// Copy response body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf(errFmtWritingFile, err)
	}

	return nil
}
