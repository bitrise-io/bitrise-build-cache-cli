package xcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/go-utils/v2/log"
)

// ErrCacheNotFound ...
var ErrCacheNotFound = errors.New("no cache archive found for the provided keys")

func download(ctx context.Context, downloadPath, key, accessToken, cacheUrl string, logger log.Logger) error {
	logger.Infof("Downloading %s from %s\n", downloadPath, cacheUrl)
	buildCacheHost, insecureGRPC, err := kv.ParseUrlGRPC(cacheUrl)
	if err != nil {
		return fmt.Errorf(
			"the url grpc[s]://host:port format, %q is invalid: %w",
			cacheUrl, err,
		)
	}

	file, err := os.Create(downloadPath)
	if err != nil {
		return fmt.Errorf("create %q: %w", downloadPath, err)
	}
	defer file.Close()

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
		return fmt.Errorf("create kv get client: %w", err)
	}
	defer kvReader.Close()

	if _, err := io.Copy(file, kvReader); err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			return ErrCacheNotFound
		}
		logger.Debugf("Failed to download archive: %s", err)
		return fmt.Errorf("failed to download archive: %w", err)
	}
	return nil
}
