package xcode

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/dustin/go-humanize"

	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-utils/v2/log"
)

// nolint: funlen, cyclop
func UploadFileToBuildCache(filePath, key, cacheURL string, authConfig common.CacheAuthConfig, logger log.Logger) error {
	buildCacheHost, insecureGRPC, err := kv.ParseURLGRPC(cacheURL)
	if err != nil {
		return fmt.Errorf(
			"the url grpc[s]://host:port format, %q is invalid: %w",
			cacheURL, err,
		)
	}

	logger.Debugf("Uploading %s to %s\n", filePath, buildCacheHost)

	checksum, err := checksumOfFile(filePath)
	if err != nil {
		logger.Warnf(err.Error())
		// fail silently and continue
	}

	const retries = 3
	err = retry.Times(retries).Wait(5 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
		if attempt != 0 {
			logger.Debugf("Retrying archive upload... (attempt %d)", attempt+1)
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
			return fmt.Errorf("new kv client: %w", err), false
		}

		err = kvClient.GetCapabilities(ctx)
		if err != nil {
			return err, false
		}

		fileSize, err := uploadFile(ctx, kvClient, filePath, key, checksum, logger)
		logger.Infof("(i) Uploaded: %s", humanize.Bytes(uint64(fileSize)))

		if err != nil {
			return err, false
		}

		return nil, false
	})
	if err != nil {
		return fmt.Errorf("with retries: %w", err)
	}

	return nil
}

func uploadFile(ctx context.Context, client *kv.Client, filePath, key, checksum string, logger log.Logger) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("open %q: %w", filePath, err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat %q: %w", filePath, err)
	}

	kvWriter, err := client.Put(ctx, kv.PutParams{
		Name:      key,
		Sha256Sum: checksum,
		FileSize:  stat.Size(),
	})
	if err != nil {
		return 0, fmt.Errorf("create kv put client (with key %s): %w", key, err)
	}
	if _, err := io.Copy(kvWriter, file); err != nil {
		return 0, fmt.Errorf("upload archive: %w", err)
	}
	if err := kvWriter.Close(); err != nil {
		return 0, fmt.Errorf("close upload: %w", err)
	}

	return stat.Size(), nil
}
