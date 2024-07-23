package xcode

import (
	"context"
	"fmt"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/dustin/go-humanize"
	"os"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

func DownloadDerivedDataFilesFromBuildCache(dd DerivedData, cacheURL string, authConfig common.CacheAuthConfig, logger log.Logger) error {
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
		return fmt.Errorf("new kv client: %w", err)
	}

	err = kvClient.GetCapabilities(ctx)
	if err != nil {
		return fmt.Errorf("failed to get capabilities: %w", err)
	}

	logger.TInfof("(i) Downloading %d files...", len(dd.Files))

	var wg sync.WaitGroup
	var mutex sync.Mutex
	semaphore := make(chan struct{}, 20) // Limit parallelization
	failedDownload := false
	var downloadSize int64
	for _, file := range dd.Files {
		wg.Add(1)
		semaphore <- struct{}{} // Block if there are too many goroutines are running

		go func(file *DerivedDataFile) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release a slot in the semaphore

			const retries = 3
			err = retry.Times(retries).Wait(3 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
				err = downloadFile(ctx, kvClient, file.AbsolutePath, file.Hash, file.Mode)
				if err != nil {
					return fmt.Errorf("download file: %w", err), false
				}

				if err := os.Chtimes(file.AbsolutePath, file.ModTime, file.ModTime); err != nil {
					return fmt.Errorf("failed to set file mod time: %w", err), true
				}

				return nil, false
			})

			mutex.Lock()
			if err != nil {
				failedDownload = true
				logger.Errorf("Failed to download file %s with error: %v", file.AbsolutePath, err)
			} else {
				downloadSize += file.Size

			}
			mutex.Unlock()
		}(file)
	}

	wg.Wait()

	logger.TInfof("(i) Downloaded: %s", humanize.Bytes(uint64(downloadSize)))

	if failedDownload {
		return fmt.Errorf("failed to download some files")
	}

	return nil
}
