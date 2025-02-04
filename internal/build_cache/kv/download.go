package kv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrCacheNotFound ...
var ErrCacheNotFound = errors.New("no cache archive found for the provided keys")

// ErrFileExistsAndNotWritable ...
var ErrFileExistsAndNotWritable = errors.New("file already exists and is not writable")

func (c *Client) DownloadFileFromBuildCache(ctx context.Context, fileName, key string) error {
	c.logger.Debugf("Downloading %s", fileName)

	_, err := c.DownloadFile(ctx, fileName, key, 0, false, false, false)

	return err
}

func (c *Client) DownloadStreamFromBuildCache(ctx context.Context, destination io.Writer, key string) error {
	c.logger.Debugf("Downloading %s", key)

	return c.DownloadStream(ctx, destination, key)
}

// nolint: nestif
func (c *Client) DownloadFile(ctx context.Context, filePath, key string, fileMode os.FileMode, isDebugLogMode, skipExisting, forceOverwrite bool) (bool, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return false, fmt.Errorf("create directory: %w", err)
	}

	if fileMode == 0 {
		fileMode = 0666
	}

	if fileInfo, err := os.Stat(filePath); err == nil {
		if skipExisting {
			return true, nil
		}

		ownerWritable := (fileInfo.Mode().Perm() & 0200) != 0
		if !ownerWritable {
			if !forceOverwrite {
				return false, ErrFileExistsAndNotWritable
			}

			if err := os.Chmod(filePath, 0666); err != nil {
				return false, fmt.Errorf("force overwrite - failed to change existing file permissions: %w", err)
			}

			if err := os.Remove(filePath); err != nil {
				return false, fmt.Errorf("force overwrite - failed to remove existing file: %w", err)
			}
		}
	}

	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		if isDebugLogMode {
			c.logFilePathDebugInfo(filePath)
		}

		return false, fmt.Errorf("create %q: %w", filePath, err)
	}
	defer file.Close()

	return false, c.DownloadStream(ctx, file, key)
}

func (c *Client) DownloadStream(ctx context.Context, destination io.Writer, key string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	kvReader, err := c.InitiateGet(timeoutCtx, key)
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

func (c *Client) logFilePathDebugInfo(filePath string) {
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		c.logger.Debugf("    File already exists - permissions: %s\n", fileInfo.Mode().String())

		if stat, ok := fileInfo.Sys().(*syscall.Stat_t); ok {
			c.logger.Debugf("    Owner UID: %d Owner GID: %d\n", stat.Uid, stat.Gid)
		}
	}

	dirPath := filepath.Dir(filePath)
	dirInfo, err := os.Stat(dirPath)
	if err == nil {
		c.logger.Debugf("    Containing dir permissions: %s\n", dirInfo.Mode().String())
		if stat, ok := dirInfo.Sys().(*syscall.Stat_t); ok {
			c.logger.Debugf("    Owner UID: %d Owner GID: %d\n", stat.Uid, stat.Gid)
		}
	}
}
