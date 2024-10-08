package xcode

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/dustin/go-humanize"

	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-utils/v2/log"
)

func UploadFileToBuildCache(ctx context.Context, filePath, key string, kvClient *kv.Client, logger log.Logger) error {
	logger.Debugf("Uploading %s", filePath)

	checksum, err := ChecksumOfFile(filePath)
	if err != nil {
		return fmt.Errorf("checksum of %s: %w", filePath, err)
	}

	err = uploadToBuildCache(ctx, kvClient, logger, func(ctx context.Context, client *kv.Client) error {
		fileSize, err := uploadFile(ctx, client, filePath, key, checksum, logger)
		logger.Infof("(i) Uploaded: %s", humanize.Bytes(uint64(fileSize)))

		return err
	})

	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	return nil
}

func UploadStreamToBuildCache(ctx context.Context, source io.Reader, key string, size int64, kvClient *kv.Client, logger log.Logger) error {
	// calculate hash from source stream first and clone it to be able to read it again for the upload
	var sourceBuf bytes.Buffer
	teeSource := io.TeeReader(source, &sourceBuf)
	checksum, err := Checksum(teeSource)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}

	if err := uploadToBuildCache(ctx, kvClient, logger, func(ctx context.Context, client *kv.Client) error {
		return uploadStream(ctx, client, &sourceBuf, key, checksum, size, logger)
	}); err != nil {
		return fmt.Errorf("upload stream: %w", err)
	}

	return nil
}

// nolint: funlen
func uploadToBuildCache(ctx context.Context, client *kv.Client, logger log.Logger, upload func(ctx context.Context, client *kv.Client) error) error {
	const retries = 3
	err := retry.Times(retries).Wait(5 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
		if attempt != 0 {
			logger.Debugf("Retrying archive upload... (attempt %d)", attempt+1)
		}

		if err := upload(ctx, client); err != nil {
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

	if err = uploadStream(ctx, client, file, key, checksum, stat.Size(), logger); err != nil {
		return 0, fmt.Errorf("upload %q: %w", filePath, err)
	}

	return stat.Size(), nil
}

func uploadStream(ctx context.Context, client *kv.Client, source io.Reader, key, checksum string, size int64, _ log.Logger) error {
	kvWriter, err := client.Put(ctx, kv.PutParams{
		Name:      key,
		Sha256Sum: checksum,
		FileSize:  size,
	})
	if err != nil {
		return fmt.Errorf("create kv put client (with key %s): %w", key, err)
	}
	if _, err := io.Copy(kvWriter, source); err != nil {
		return fmt.Errorf("upload archive: %w", err)
	}
	if err := kvWriter.Close(); err != nil {
		return fmt.Errorf("close upload: %w", err)
	}

	return nil
}
