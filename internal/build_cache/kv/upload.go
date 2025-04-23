package kv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/hash"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/dustin/go-humanize"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (c *Client) UploadFileToBuildCache(ctx context.Context, filePath, key string) error {
	c.logger.Debugf("Uploading %s", filePath)

	checksum, err := hash.ChecksumOfFile(filePath)
	if err != nil {
		return fmt.Errorf("checksum of %s: %w", filePath, err)
	}

	err = c.uploadToBuildCache(ctx, func(ctx context.Context) error {
		fileSize, err := c.uploadFile(ctx, filePath, key, checksum)
		//nolint: gosec
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
			c.logger.Debugf("Retrying upload... (attempt %d)", attempt)
		}

		if err := upload(ctx); err != nil {
			c.logger.Errorf("Error in upload attempt %d: %s", attempt, err)

			if errors.Is(err, ErrCacheUnauthenticated) {
				return ErrCacheUnauthenticated, true
			}

			return fmt.Errorf("upload: %w", err), false
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

	if size > 0 {
		_, err = io.Copy(kvWriter, source)
	} else {
		// io.Copy does not write if there was no read
		_, err = kvWriter.Write([]byte{})
	}

	st, ok := status.FromError(err)
	if ok && st.Code() == codes.Unauthenticated {
		return ErrCacheUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("upload archive: %w", err)
	}

	if err := kvWriter.Close(); err != nil {
		return fmt.Errorf("close upload: %w", err)
	}

	return nil
}
