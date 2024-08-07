package xcode

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/dustin/go-humanize"

	"errors"

	"sync/atomic"

	"github.com/bitrise-io/go-utils/v2/log"
)

type DownloadFilesStats struct {
	FilesToBeDownloaded   int
	FilesDownloaded       int
	FilesMissing          int
	FilesFailedToDownload int
	DownloadSize          int64
	LargestFileSize       int64
}

func DownloadCacheFilesFromBuildCache(ctx context.Context, dd FileGroupInfo, kvClient *kv.Client, logger log.Logger) (DownloadFilesStats, error) {
	var largestFileSize int64
	for _, file := range dd.Files {
		if file.Size > largestFileSize {
			largestFileSize = file.Size
		}
	}

	logger.TInfof("(i) Downloading %d files, largest is %s",
		len(dd.Files), humanize.Bytes(uint64(largestFileSize)))

	var filesDownloaded atomic.Int32
	var filesMissing atomic.Int32
	var filesFailedToDownload atomic.Int32
	var downloadSize atomic.Int64

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 20) // Limit parallelization
	for _, file := range dd.Files {
		wg.Add(1)
		semaphore <- struct{}{} // Block if there are too many goroutines are running

		go func(file *FileInfo) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release a slot in the semaphore

			const retries = 3
			err := retry.Times(retries).Wait(3 * time.Second).TryWithAbort(func(_ uint) (error, bool) {
				err := downloadFile(ctx, kvClient, file.Path, file.Hash, file.Mode)
				if errors.Is(err, ErrCacheNotFound) {
					return err, true
				} else if err != nil {
					return fmt.Errorf("download file: %w", err), false
				}

				if err := os.Chtimes(file.Path, file.ModTime, file.ModTime); err != nil {
					return fmt.Errorf("failed to set file mod time: %w", err), true
				}

				return nil, false
			})

			switch {
			case errors.Is(err, ErrCacheNotFound):
				logger.Infof("Cache entry not found for file %s (%s)", file.Path, file.Hash)

				filesMissing.Add(1)
			case err != nil:
				logger.Errorf("Failed to download file %s with error: %v", file.Path, err)

				filesFailedToDownload.Add(1)
			default:
				filesDownloaded.Add(1)
				downloadSize.Add(file.Size)
			}
		}(file)
	}

	wg.Wait()

	logger.TInfof("(i) Downloaded: %d files (%s). Missing: %d files", filesDownloaded.Load(), humanize.Bytes(uint64(downloadSize.Load())), filesMissing.Load())

	stats := DownloadFilesStats{
		FilesToBeDownloaded:   len(dd.Files),
		FilesDownloaded:       int(filesDownloaded.Load()),
		FilesMissing:          int(filesMissing.Load()),
		FilesFailedToDownload: int(filesFailedToDownload.Load()),
		DownloadSize:          downloadSize.Load(),
		LargestFileSize:       largestFileSize,
	}
	logger.Debugf("Download stats:")
	logger.Debugf("  Files to be downloaded: %d", len(dd.Files))
	logger.Debugf("  Files downloaded: %d", int(filesDownloaded.Load()))
	logger.Debugf("  Files missing: %d", int(filesMissing.Load()))
	logger.Debugf("  Files failed to download: %d", int(filesFailedToDownload.Load()))
	logger.Debugf("  Download size: %s", humanize.Bytes(uint64(downloadSize.Load())))
	logger.Debugf("  Largest file size: %s", humanize.Bytes(uint64(largestFileSize)))

	if filesFailedToDownload.Load() > 0 || filesMissing.Load() > 0 {
		return stats, fmt.Errorf("failed to download some files")
	}

	return stats, nil
}
