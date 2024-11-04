package kv

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/retry"
	"github.com/dustin/go-humanize"

	"errors"

	"sync/atomic"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/filegroup"
)

type DownloadFilesStats struct {
	FilesToBeDownloaded   int
	FilesDownloaded       int
	FilesMissing          int
	FilesFailedToDownload int
	DownloadSize          int64
	LargestFileSize       int64
}

// nolint: gocognit
func (c *Client) DownloadFileGroupFromBuildCache(ctx context.Context, dd filegroup.Info,
	isDebugLogMode, skipExisting, forceOverwrite bool, maxLoggedDownloadErrors int) (DownloadFilesStats, error) {
	var largestFileSize int64
	for _, file := range dd.Files {
		if file.Size > largestFileSize {
			largestFileSize = file.Size
		}
	}

	c.logger.TInfof("(i) Downloading %d files, largest is %s",
		len(dd.Files), humanize.Bytes(uint64(largestFileSize)))

	var filesDownloaded atomic.Int32
	var filesMissing atomic.Int32
	var filesFailedToDownload atomic.Int32
	var downloadSize atomic.Int64
	var skippedFiles atomic.Int32

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 20) // Limit parallelization
	for _, file := range dd.Files {
		wg.Add(1)
		semaphore <- struct{}{} // Block if there are too many goroutines are running

		go func(file *filegroup.FileInfo) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release a slot in the semaphore

			const retries = 3
			err := retry.Times(retries).Wait(3 * time.Second).TryWithAbort(func(_ uint) (error, bool) {
				skipped, err := c.DownloadFile(ctx, file.Path, file.Hash, file.Mode, isDebugLogMode, forceOverwrite, skipExisting)
				if skipped {
					skippedFiles.Add(1)

					return nil, false
				}

				if errors.Is(err, ErrCacheNotFound) {
					c.logger.Debugf("Cache entry not found for file %s (%s)", file.Path, file.Hash)
					return err, true
				} else if errors.Is(err, ErrFileExistsAndNotWritable) {
					return err, true
				}
				if err != nil {
					return fmt.Errorf("download file: %w", err), false
				}

				if err := os.Chtimes(file.Path, file.ModTime, file.ModTime); err != nil {
					return fmt.Errorf("failed to set file mod time: %w", err), true
				}

				return nil, false
			})

			missingPlusFailed := filesMissing.Load() + filesFailedToDownload.Load()
			switch {
			case errors.Is(err, ErrCacheNotFound):
				if missingPlusFailed < int32(maxLoggedDownloadErrors) {
					c.logger.Infof("Cache entry not found for file %s (%s)", file.Path, file.Hash)
				}

				filesMissing.Add(1)
			case err != nil:
				if missingPlusFailed < int32(maxLoggedDownloadErrors) {
					c.logger.Errorf("Failed to download file %s with error: %v", file.Path, err)
				}

				filesFailedToDownload.Add(1)
			default:
				filesDownloaded.Add(1)
				downloadSize.Add(file.Size)
			}
		}(file)
	}

	wg.Wait()

	c.logger.TInfof("(i) Downloaded: %d files (%s). Missing: %d files. Failed: %d files", filesDownloaded.Load(), humanize.Bytes(uint64(downloadSize.Load())), filesMissing.Load(), filesFailedToDownload.Load())

	stats := DownloadFilesStats{
		FilesToBeDownloaded:   len(dd.Files) - int(skippedFiles.Load()),
		FilesDownloaded:       int(filesDownloaded.Load()),
		FilesMissing:          int(filesMissing.Load()),
		FilesFailedToDownload: int(filesFailedToDownload.Load()),
		DownloadSize:          downloadSize.Load(),
		LargestFileSize:       largestFileSize,
	}
	c.logger.Debugf("Download stats:")
	c.logger.Debugf("  Files to be downloaded: %d", stats.FilesToBeDownloaded)
	c.logger.Debugf("  Files downloaded: %d", stats.FilesDownloaded)
	c.logger.Debugf("  Files missing: %d", stats.FilesMissing)
	c.logger.Debugf("  Files failed to download: %d", stats.FilesFailedToDownload)
	c.logger.Debugf("  Files skipped (existing): %d", skippedFiles.Load())
	c.logger.Debugf("  Download size: %s", humanize.Bytes(uint64(stats.DownloadSize)))
	c.logger.Debugf("  Largest file size: %s", humanize.Bytes(uint64(stats.LargestFileSize)))

	if maxLoggedDownloadErrors < stats.FilesFailedToDownload+stats.FilesMissing {
		c.logger.Warnf("Too many download errors or missing files, only the first %d errors were logged", maxLoggedDownloadErrors)
	}

	if stats.FilesFailedToDownload > 0 || stats.FilesMissing > 0 {
		return stats, fmt.Errorf("failed to download some files")
	}

	return stats, nil
}
