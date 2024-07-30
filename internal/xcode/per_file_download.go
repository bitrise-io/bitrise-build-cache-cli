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

	"github.com/bitrise-io/go-utils/v2/log"
)

type DownloadFilesStats struct {
	FilesToBeDownloaded   int
	FilesDownloaded       int
	FilesMissing          int
	FilesFailedToDownload int
	DownloadSize          int64
}

func DownloadCacheFilesFromBuildCache(dd FileGroupInfo, kvClient *kv.Client, logger log.Logger) (DownloadFilesStats, error) {
	var largestFileSize int64
	for _, file := range dd.Files {
		if file.Size > largestFileSize {
			largestFileSize = file.Size
		}
	}

	ctx, _ := context.WithCancel(context.Background())
	// TODO context cancellation

	logger.TInfof("(i) Downloading %d files, largest is %s",
		len(dd.Files), humanize.Bytes(uint64(largestFileSize)))

	stats := DownloadFilesStats{
		FilesToBeDownloaded: len(dd.Files),
	}

	var wg sync.WaitGroup
	var mutex sync.Mutex
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
					logger.Infof("cache entry not found for file %s (%s)", file.Path, file.Hash)

					stats.FilesMissing++

					return nil, true
				}
				if err != nil {
					return fmt.Errorf("download file: %w", err), false
				}

				if err := os.Chtimes(file.Path, file.ModTime, file.ModTime); err != nil {
					return fmt.Errorf("failed to set file mod time: %w", err), true
				}

				return nil, false
			})

			mutex.Lock()
			if err != nil {
				logger.Errorf("Failed to download file %s with error: %v", file.Path, err)

				stats.FilesFailedToDownload++
			} else {
				stats.FilesDownloaded++
				stats.DownloadSize += file.Size
			}
			mutex.Unlock()
		}(file)
	}

	wg.Wait()

	logger.TInfof("(i) Downloaded: %s", humanize.Bytes(uint64(stats.DownloadSize)))

	if stats.FilesFailedToDownload > 0 {
		return DownloadFilesStats{}, fmt.Errorf("failed to download some files")
	}

	return stats, nil
}
