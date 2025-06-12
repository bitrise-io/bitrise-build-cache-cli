package gradle

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/bitrise-io/go-utils/retry"
)

var (
	errFmtDownloading = "Error while downloading %w"
	errFmtBadStatus   = "Bad status: %s"
	errFmtCreateFile  = "Error while creating file %w"
	errFmtWritingFile = "Error while writing file %w"
)

type GradlePluginFileDownloader struct {
	fileName      string
	repositoryURL string
	downloadPath  string
}

func (downloader GradlePluginFileDownloader) Download() error {
	const retries = 3
	const timeout = 30 * time.Second

	err := retry.
		Times(retries).
		Wait(timeout).
		TryWithAbort(func(attempt uint) (error, bool) {
			resp, err := http.Get(downloader.repositoryURL + "/" + downloader.fileName)
			if err != nil {
				return fmt.Errorf(errFmtDownloading, err), true
			}
			defer func(Body io.ReadCloser) {
				if err := Body.Close(); err != nil {
					fmt.Println("Error closing file reader")
				}
			}(resp.Body)

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf(errFmtBadStatus, resp.Status), true
			}

			// Create the file
			out, err := os.Create(downloader.downloadPath)
			if err != nil {
				return fmt.Errorf(errFmtCreateFile, err), true
			}
			defer func(out *os.File) {
				if err := out.Close(); err != nil {
					fmt.Println("Error closing file")
				}
			}(out)

			// Copy response body to file
			_, err = io.Copy(out, resp.Body)
			if err != nil {
				return fmt.Errorf(errFmtWritingFile, err), true
			}

			return nil, false
		})

	return err
}
