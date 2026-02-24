package ccache

import (
	"context"
	"io"

	"github.com/bitrise-io/go-utils/v2/log"
)

// Client is the interface for interacting with the remote build cache.
type Client interface {
	DownloadStream(ctx context.Context, writer io.Writer, key string) error
	UploadStreamToBuildCache(ctx context.Context, reader io.ReadSeeker, key string, size int64) error
	GetCapabilitiesWithRetry(ctx context.Context) error
}

// LoggerFactory creates a logger for a given invocation ID.
type LoggerFactory func(invocationID string) (log.Logger, error)
