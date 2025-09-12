package kv

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bitrise-io/go-utils/retry"
	"github.com/dustin/go-humanize"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/hash"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/slicebuf"
)

func (c *Client) UploadFileToBuildCache(ctx context.Context, filePath, key string) error {
	c.logger.Debugf("Uploading %s", filePath)

	checksum, err := hash.ChecksumOfFile(filePath)
	if err != nil {
		return fmt.Errorf("checksum of %s: %w", filePath, err)
	}

	fileSize, err := c.uploadFile(ctx, filePath, key, checksum)
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	//nolint: gosec
	c.logger.Infof("(i) Uploaded: %s", humanize.Bytes(uint64(fileSize)))

	return nil
}

func (c *Client) UploadStreamToBuildCache(ctx context.Context, source io.Reader, key string, size int64) error {
	// calculate hash from source stream first and clone it to be able to read it again for the upload
	sourceBuf := slicebuf.NewBuffer()
	teeSource := io.TeeReader(source, sourceBuf)
	checksum, err := hash.Checksum(teeSource)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}

	if err := c.uploadStream(ctx, sourceBuf, key, checksum, size); err != nil {
		return fmt.Errorf("upload stream: %w", err)
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

func (c *Client) uploadStream(ctx context.Context, source io.ReadSeeker, key, checksum string, size int64) error {
	const divisor = 10 * 1024 * 1024 // 10 MB

	// give each 10 MB a second, min 20s max 2m
	timeout := min(
		max(
			time.Duration(size/divisor)*time.Second,
			20*time.Second,
		),
		2*time.Minute,
	)

	var n int64

	//nolint:wrapcheck
	return retry.Times(c.uploadRetry).Wait(c.uploadRetryWait).TryWithAbort(func(attempt uint) (error, bool) {
		if attempt == 0 {
			c.logger.Debugf("Uploading %s (size: %d, timeout: %s)", key, size, timeout.String())
		} else {
			c.logger.Infof("%d. attempt to upload %s (size: %d, previously uploaded: %d, timeout: %s)", attempt+1, key, size, n, timeout.String())
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		kvWriter, err := c.initiatePut(timeoutCtx, PutParams{
			Name:      key,
			Sha256Sum: checksum,
			FileSize:  size,
		})
		if err != nil {
			c.logger.Warnf("Failed to upload stream: attempt %d: initiate put: %s", attempt+1, err)

			return fmt.Errorf("create kv put client (with key %s): %w", key, err), false
		}

		if size > 0 {
			if _, err := source.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("seek source to start: %w", err), true
			}

			// io.Copy does not write if there was no read
			n, err = io.Copy(kvWriter, source)
		} else {
			// io.Copy does not write if there was no read
			_, err = kvWriter.Write([]byte{})
		}

		st, ok := status.FromError(err)
		if ok && st.Code() == codes.Unauthenticated {
			return ErrCacheUnauthenticated, true
		}
		if err != nil {
			c.logger.Warnf("Failed to upload stream: attempt %d: %s", attempt+1, err)

			return fmt.Errorf("upload archive: %w", err), false
		}

		if err := kvWriter.Close(); err != nil {
			return fmt.Errorf("close upload: %w", err), false
		}

		if kvWriter.Response().GetCommittedSize() != size {
			return fmt.Errorf("uploaded size mismatch: expected %d, got %d", size, kvWriter.Response().GetCommittedSize()), false
		}

		return nil, false
	})
}
