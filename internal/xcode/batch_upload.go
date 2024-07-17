package xcode

import (
	"context"
	"fmt"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/dustin/go-humanize"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/go-utils/v2/log"
)

func UploadDerivedDataFilesToBuildCache(dd DerivedData, cacheURL string, authConfig common.CacheAuthConfig, logger log.Logger) error {
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

	missingBlobs, err := findMissingBlobs(ctx, dd, kvClient, logger)
	if err != nil {
		return fmt.Errorf("failed to check for missing blobs: %w", err)
	}

	var totalSize int64
	uploadCount := 0
	for _, file := range dd.Files {
		if _, ok := missingBlobs[file.Hash]; ok {
			const retries = 2
			err = retry.Times(retries).Wait(3 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
				if attempt != 0 {
					logger.Debugf("Retrying archive upload... (attempt %d)", attempt+1)
				}
				fileSize, err := uploadFile(ctx, kvClient, file.AbsolutePath, file.Hash, file.Hash, logger)
				if err != nil {
					return fmt.Errorf("failed to upload file %s: %w", file.AbsolutePath, err), false
				}

				totalSize += fileSize
				uploadCount++

				return nil, false
			})

			if err != nil {
				return fmt.Errorf("with retries: %w", err)
			}
		}
	}

	logger.Infof("(i) Uploaded %s in %d keys", humanize.Bytes(uint64(totalSize)), uploadCount)

	return nil
}

func findMissingBlobs(ctx context.Context, dd DerivedData, client *kv.Client, logger log.Logger) (map[string]bool, error) {
	logger.Infof("(i) Checking for missing blobs in the cache of %d files", len(dd.Files))

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

	logger.Debugf("(i) Checking %d files", len(allDigests))
	missingDigests, err := client.FindMissing(ctx, allDigests)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing files in the cache: %w", err)
	}

	missingBlobs := make(map[string]bool)
	for _, d := range missingDigests {
		missingBlobs[d.Sha256Sum] = true
	}

	logger.Infof("(i) %d of %d blobs are missing", len(missingBlobs), len(blobs))

	return missingBlobs, nil
}
