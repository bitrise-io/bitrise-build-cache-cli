package xcode

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/retry"
	"github.com/dustin/go-humanize"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/go-utils/v2/log"
)

func UploadCacheFilesToBuildCache(dd CacheDirectoryMetadata, cacheURL string, authConfig common.CacheAuthConfig, logger log.Logger) error {
	buildCacheHost, insecureGRPC, err := kv.ParseURLGRPC(cacheURL)
	if err != nil {
		return fmt.Errorf(
			"the url grpc[s]://host:port format, %q is invalid: %w",
			cacheURL, err,
		)
	}

	ctx := context.Background()
	kvClient, err := kv.NewClient(ctx, kv.NewClientParams{
		UseInsecure: insecureGRPC,
		Host:        buildCacheHost,
		DialTimeout: 5 * time.Second,
		ClientName:  "kv",
		AuthConfig:  authConfig,
	})
	if err != nil {
		return fmt.Errorf("new KV client: %w", err)
	}

	err = kvClient.GetCapabilities(ctx)
	if err != nil {
		return fmt.Errorf("failed to get capabilities: %w", err)
	}

	missingBlobs, err := findMissingBlobs(ctx, dd, kvClient, logger)
	if err != nil {
		return fmt.Errorf("failed to check for missing blobs: %w", err)
	}

	logger.TInfof("(i) Uploading missing blobs...")

	var totalSize int64
	uploadCount := 0
	var wg sync.WaitGroup
	var mutex sync.Mutex
	semaphore := make(chan struct{}, 20) // Limit parallelization
	failedUpload := false
	for _, file := range dd.Files {
		mutex.Lock()
		_, ok := missingBlobs[file.Hash]
		mutex.Unlock()
		if ok {
			wg.Add(1)
			semaphore <- struct{}{} // Block if there are too many goroutines are running

			go func(file *FileInfo) {
				defer wg.Done()
				defer func() { <-semaphore }() // Release a slot in the semaphore

				const retries = 2
				err = retry.Times(retries).Wait(3 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
					if attempt != 0 {
						logger.Debugf("Retrying archive upload... (attempt %d)", attempt+1)
					}
					fileSize, err := uploadFile(ctx, kvClient, file.Path, file.Hash, file.Hash, logger)
					if err != nil {
						return fmt.Errorf("failed to upload file %s: %w", file.Path, err), false
					}

					mutex.Lock()
					// Delete the uploded blob from the map of the missing blobs
					delete(missingBlobs, file.Hash)

					totalSize += fileSize
					uploadCount++
					mutex.Unlock()

					return nil, false
				})

				if err != nil {
					failedUpload = true
					logger.Errorf("Failed to upload file %s with error: %v", file.Path, err)
				}
			}(file)
		}
	}

	wg.Wait()

	logger.TInfof("(i) Uploaded %s in %d keys", humanize.Bytes(uint64(totalSize)), uploadCount)

	if failedUpload {
		return fmt.Errorf("failed to upload some files")
	}

	return nil
}

func findMissingBlobs(ctx context.Context, dd CacheDirectoryMetadata, client *kv.Client, logger log.Logger) (map[string]bool, error) {
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
