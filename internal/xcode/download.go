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
	"github.com/bitrise-io/go-utils/v2/log"
)

// ErrCacheNotFound ...
var ErrCacheNotFound = errors.New("no cache archive found for the provided keys")

func DownloadFromBuildCache(fileName, key, accessToken, cacheURL string, logger log.Logger) error {
	logger.Debugf("Downloading %s from %s", fileName, cacheURL)

	buildCacheHost, insecureGRPC, err := kv.ParseURLGRPC(cacheURL)
	if err != nil {
		return fmt.Errorf(
			"the url grpc[s]://host:port format, %q is invalid: %w",
			cacheURL, err,
		)
	}

	dir := filepath.Dir(fileName)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	file, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("create %q: %w", fileName, err)
	}
	defer file.Close()

	ctx := context.Background()
	kvClient, err := kv.NewClient(ctx, kv.NewClientParams{
		UseInsecure: insecureGRPC,
		Host:        buildCacheHost,
		DialTimeout: 5 * time.Second,
		ClientName:  "kv",
		Token:       accessToken,
	})
	if err != nil {
		return fmt.Errorf("new kv client: %w", err)
	}

	kvReader, err := kvClient.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("create kv get client (with key %s): %w", key, err)
	}
	defer kvReader.Close()

	if _, err := io.Copy(file, kvReader); err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			return ErrCacheNotFound
		}

		return fmt.Errorf("failed to download archive: %w", err)
	}

	return nil
}
