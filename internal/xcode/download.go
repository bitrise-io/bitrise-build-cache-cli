package xcode

import (
	"context"
	"errors"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/go-utils/v2/log"
)

// ErrCacheNotFound ...
var ErrCacheNotFound = errors.New("no cache archive found for the provided keys")

func DownloadFileFromBuildCache(fileName, key string, kvClient *kv.Client, logger log.Logger) error {
	logger.Debugf("Downloading %s", fileName)

	ctx, _ := context.WithCancel(context.Background())
	// TODO context cancellation

	return downloadFile(ctx, kvClient, fileName, key, 0)
}

func DownloadStreamFromBuildCache(destination io.Writer, key string, kvClient *kv.Client, logger log.Logger) error {
	logger.Debugf("Downloading %s", key)

	ctx, _ := context.WithCancel(context.Background())
	// TODO context cancellation

	return downloadStream(ctx, destination, kvClient, key)
}

func downloadFile(ctx context.Context, client *kv.Client, filePath, key string, fileMode os.FileMode) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if fileMode == 0 {
		fileMode = 0666
	}
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("create %q: %w", filePath, err)
	}
	defer file.Close()

	return downloadStream(ctx, file, client, key)
}

func downloadStream(ctx context.Context, destination io.Writer, client *kv.Client, key string) error {
	kvReader, err := client.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("create kv get client (with key %s): %w", key, err)
	}
	defer kvReader.Close()

	if _, err := io.Copy(destination, kvReader); err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			return ErrCacheNotFound
		}

		return fmt.Errorf("download archive: %w", err)
	}

	return nil
}
