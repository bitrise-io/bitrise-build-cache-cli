package xcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/go-utils/v2/log"
)

// ErrCacheNotFound ...
var ErrCacheNotFound = errors.New("no cache archive found for the provided keys")

func DownloadFromBuildCache(fileName, key, cacheURL string, authConfig common.CacheAuthConfig, logger log.Logger) error {
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
		return err
	}

	logger.Debugf("Downloading %s from %s", fileName, buildCacheHost)

	err = downloadFile(ctx, kvClient, fileName, key, 0, true)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}

	return nil
}

func downloadFile(ctx context.Context, client *kv.Client, filePath, key string, fileMode os.FileMode, createDirectories bool) error {
	dir := filepath.Dir(filePath)
	if createDirectories {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	if fileMode == 0 {
		fileMode = 0666
	}
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("create %q: %w", filePath, err)
	}
	defer file.Close()

	kvReader, err := client.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("create kv get client (with key %s): %w", key, err)
	}
	defer kvReader.Close()

	if _, err := io.Copy(file, kvReader); err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			return ErrCacheNotFound
		}

		return fmt.Errorf("download archive: %w", err)
	}

	return nil

}
