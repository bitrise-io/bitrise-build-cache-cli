package kv

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/hash"
	"github.com/bitrise-io/go-utils/retry"
)

func (c *Client) UploadFileToBuildCache(ctx context.Context, filePath, key string) error {
	c.logger.Debugf("Uploading %s", filePath)

	checksum, err := hash.ChecksumOfFile(filePath)
	if err != nil {
		return fmt.Errorf("checksum of %s: %w", filePath, err)
	}

	err = c.uploadToBuildCache(ctx, func(ctx context.Context) error {
		fileSize, err := c.uploadFile(ctx, filePath, key, checksum)
		c.logger.Infof("(i) Uploaded: %s", humanize.Bytes(uint64(fileSize)))

		return err
	})

	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	return nil
}

func (c *Client) UploadStreamToBuildCache(ctx context.Context, source io.Reader, key string, size int64) error {
	// calculate hash from source stream first and clone it to be able to read it again for the upload
	var sourceBuf bytes.Buffer
	teeSource := io.TeeReader(source, &sourceBuf)
	checksum, err := hash.Checksum(teeSource)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}

	if err := c.uploadToBuildCache(ctx, func(ctx context.Context) error {
		return c.uploadStream(ctx, &sourceBuf, key, checksum, size)
	}); err != nil {
		return fmt.Errorf("upload stream: %w", err)
	}

	return nil
}

func (c *Client) uploadToBuildCache(ctx context.Context, upload func(ctx context.Context) error) error {
	const retries = 3
	err := retry.Times(retries).Wait(5 * time.Second).TryWithAbort(func(attempt uint) (error, bool) {
		if attempt != 0 {
			c.logger.Debugf("Retrying archive upload... (attempt %d)", attempt+1)
		}

		if err := upload(ctx); err != nil {
			return err, false
		}

		return nil, false
	})
	if err != nil {
		return fmt.Errorf("with retries: %w", err)
	}

	return nil
}

func (c *Client) uploadFile(ctx context.Context, filePath, key, checksum string) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("open %q: %w", filePath, err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat %q: %w", filePath, err)
	}

	if err = c.uploadStream(ctx, file, key, checksum, stat.Size()); err != nil {
		return 0, fmt.Errorf("upload %q: %w", filePath, err)
	}

	return stat.Size(), nil
}

func (c *Client) uploadStream(ctx context.Context, source io.Reader, key, checksum string, size int64) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	kvWriter, err := c.InitiatePut(timeoutCtx, PutParams{
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
