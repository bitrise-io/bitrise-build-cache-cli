package xcode

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/retry"
	"github.com/dustin/go-humanize"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/go-utils/v2/log"
)

type UploadFilesStats struct {
	FilesToUpload       int
	FilesUploaded       int
	FilesFailedToUpload int
	TotalFiles          int
	UploadSize          int64
	LargestFileSize     int64
}

func uploadCacheFileToBuildCache(ctx context.Context, kvClient *kv.Client, file *FileInfo, mutex *sync.Mutex, stats *UploadFilesStats, logger log.Logger) {
	const retries = 2
	err := retry.Times(retries).Wait(3 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
		if attempt != 0 {
			logger.Debugf("Retrying archive upload... (attempt %d)", attempt+1)
		}
		_, err := uploadFile(ctx, kvClient, file.Path, file.Hash, file.Hash, logger)
		if err != nil {
			return fmt.Errorf("failed to upload file %s: %w", file.Path, err), false
		}

		return nil, false
	})

	mutex.Lock()
	if err != nil {
		logger.Errorf("Failed to upload file %s with error: %v", file.Path, err)
		stats.FilesFailedToUpload++
	} else {
		stats.FilesUploaded++
		stats.UploadSize += file.Size
		if file.Size > stats.LargestFileSize {
			stats.LargestFileSize = file.Size
		}
	}
	mutex.Unlock()
}

func UploadCacheFilesToBuildCache(ctx context.Context, dd FileGroupInfo, kvClient *kv.Client, logger log.Logger) (UploadFilesStats, error) {
	missingBlobs, err := findMissingBlobs(ctx, dd, kvClient, logger)
	if err != nil {
		return UploadFilesStats{}, fmt.Errorf("failed to check for missing blobs: %w", err)
	}

	stats := UploadFilesStats{
		TotalFiles:    len(dd.Files),
		FilesToUpload: len(missingBlobs),
	}

	logger.TInfof("(i) Uploading missing blobs...")

	var wg sync.WaitGroup
	var mutex sync.Mutex
	semaphore := make(chan struct{}, 20) // Limit parallelization
	for _, file := range dd.Files {
		mutex.Lock()
		_, ok := missingBlobs[file.Hash]
		delete(missingBlobs, file.Hash) // Remove the blob from the list of missing blobs as it's being uploaded
		mutex.Unlock()
		if !ok {
			continue
		}

		wg.Add(1)
		semaphore <- struct{}{} // Block if there are too many goroutines are running

		go func(file *FileInfo) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release a slot in the semaphore

			uploadCacheFileToBuildCache(ctx, kvClient, file, &mutex, &stats, logger)
		}(file)
	}

	wg.Wait()

	logger.TInfof("(i) Uploaded %s in %d keys", humanize.Bytes(uint64(stats.UploadSize)), stats.FilesUploaded)

	if stats.FilesFailedToUpload > 0 {
		return stats, fmt.Errorf("failed to upload some files")
	}

	return stats, nil
}

func findMissingBlobs(ctx context.Context, dd FileGroupInfo, client *kv.Client, logger log.Logger) (map[string]bool, error) {
	logger.TInfof("(i) Checking for missing blobs in the cache of %d files", len(dd.Files))

	blobs := make(map[string]bool)

	allDigests := make([]*kv.FileDigest, 0, len(dd.Files))
	for _, file := range dd.Files {
		if _, ok := blobs[file.Hash]; !ok {
			allDigests = append(allDigests, &kv.FileDigest{
				Sha256Sum:   file.Hash,
				SizeInBytes: file.Size,
			})

			blobs[file.Hash] = true
		}
	}

	logger.Infof("(i) The files are stored in %d different blobs", len(allDigests))
	missingDigests, err := client.FindMissing(ctx, allDigests)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing files in the cache: %w", err)
	}

	missingBlobs := make(map[string]bool)
	for _, d := range missingDigests {
		missingBlobs[d.Sha256Sum] = true
	}

	logger.TInfof("(i) %d of %d blobs are missing", len(missingBlobs), len(blobs))

	return missingBlobs, nil
}
