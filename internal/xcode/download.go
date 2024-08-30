package xcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/go-utils/v2/log"
)

// ErrCacheNotFound ...
var ErrCacheNotFound = errors.New("no cache archive found for the provided keys")

// ErrFileExistsAndNotWritable ...
var ErrFileExistsAndNotWritable = errors.New("file already exists and is not writable")

func DownloadFileFromBuildCache(ctx context.Context, fileName, key string, kvClient *kv.Client, logger log.Logger) error {
	logger.Debugf("Downloading %s", fileName)

	return downloadFile(ctx, kvClient, fileName, key, 0, logger, false, false)
}

func DownloadStreamFromBuildCache(ctx context.Context, destination io.Writer, key string, kvClient *kv.Client, logger log.Logger) error {
	logger.Debugf("Downloading %s", key)

	return downloadStream(ctx, destination, kvClient, key)
}

// nolint: nestif
func downloadFile(ctx context.Context, client *kv.Client, filePath, key string, fileMode os.FileMode, logger log.Logger, isDebugLogMode, forceOverwrite bool) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if fileMode == 0 {
		fileMode = 0666
	}

	if fileInfo, err := os.Stat(filePath); err == nil {
		ownerWritable := (fileInfo.Mode().Perm() & 0200) != 0
		if !ownerWritable {
			if forceOverwrite {
				if err := os.Chmod(filePath, 0666); err != nil {
					return fmt.Errorf("force overwrite - failed to change existing file permissions: %w", err)
				}

				if err := os.Remove(filePath); err != nil {
					return fmt.Errorf("force overwrite - failed to remove existing file: %w", err)
				}
			} else {
				return ErrFileExistsAndNotWritable
			}
		}
	}

	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		if isDebugLogMode {
			logFilePathDebugInfo(filePath, logger)
		}

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

func logFilePathDebugInfo(filePath string, logger log.Logger) {
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		logger.Debugf("    File already exists - permissions: %s\n", fileInfo.Mode().String())

		if stat, ok := fileInfo.Sys().(*syscall.Stat_t); ok {
			logger.Debugf("    Owner UID: %d Owner GID: %d\n", stat.Uid, stat.Gid)
		}
	}

	dirPath := filepath.Dir(filePath)
	dirInfo, err := os.Stat(dirPath)
	if err == nil {
		logger.Debugf("    Containing dir permissions: %s\n", dirInfo.Mode().String())
		if stat, ok := dirInfo.Sys().(*syscall.Stat_t); ok {
			logger.Debugf("    Owner UID: %d Owner GID: %d\n", stat.Uid, stat.Gid)
		}
	}
}
