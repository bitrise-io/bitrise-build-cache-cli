package xcode

import (
	"context"
	"fmt"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"os"
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

	//var wg sync.WaitGroup
	//var mutex sync.Mutex
	//semaphore := make(chan struct{}, 20) // Limit parallelization
	//failedDownload := false
	for _, file := range dd.Files {
		err = downloadFile(ctx, kvClient, file.AbsolutePath, file.Hash, logger)
		if err != nil {
			return fmt.Errorf("download file: %w", err)
		}

		if err := os.Chtimes(file.AbsolutePath, file.ModTime, file.ModTime); err != nil {
			return fmt.Errorf("failed to set file mod time: %w", err)
		}
	}

	return nil
}
