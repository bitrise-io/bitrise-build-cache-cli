package analytics

import (
	"encoding/json"
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
	httpClient  *retryablehttp.Client
	baseURL     string
	accessToken string
}

func NewClient(baseURL string, accessToken string, logger log.Logger) (*Client, error) {
	httpClient := retryhttp.NewClient(logger)
	httpClient.RetryMax = maxHTTPClientRetries

	return &Client{
		httpClient:  httpClient,
		baseURL:     baseURL,
		accessToken: accessToken,
	}, nil
}

func (c *Client) PutCacheOperation(op *CacheOperation) error {
	requestURL := fmt.Sprintf("%s/operations/%s", c.baseURL, op.OperationID)

	payload, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal cache operation: %w", err)
	}

	req, err := retryablehttp.NewRequest(http.MethodPut, requestURL, payload)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return unwrapError(resp)
	}

	return nil
}

func unwrapError(resp *http.Response) error {
	errorResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("HTTP %d: failed to read response body: %w", resp.StatusCode, err)
	}

	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, errorResp)
}
