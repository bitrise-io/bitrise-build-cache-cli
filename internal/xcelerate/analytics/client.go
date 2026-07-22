package analytics

import (
	"fmt"
	"io"
	"net/http"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
	"github.com/hashicorp/go-retryablehttp"
)

const (
	maxHTTPClientRetries = 3
)

type Client struct {
	httpClient    *retryablehttp.Client
	baseURL       string
	tokenSupplier func() string
	logger        log.Logger
}

// NewClient wires a supplier so each request reads a freshly-resolved token.
func NewClient(baseURL string, tokenSupplier func() string, logger log.Logger) (*Client, error) {
	httpClient := retryhttp.NewClient(logger)
	httpClient.RetryMax = maxHTTPClientRetries

	if tokenSupplier == nil {
		tokenSupplier = func() string { return "" }
	}

	return &Client{
		httpClient:    httpClient,
		baseURL:       baseURL,
		tokenSupplier: tokenSupplier,
		logger:        logger,
	}, nil
}

func unwrapError(resp *http.Response) error {
	errorResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("HTTP %d: failed to read response body: %w", resp.StatusCode, err)
	}

	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, errorResp)
}
