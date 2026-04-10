package multiplatform

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
	"github.com/hashicorp/go-retryablehttp"
)

const maxHTTPClientRetries = 3

// Client sends analytics payloads to the Bitrise backend.
type Client struct {
	httpClient  *retryablehttp.Client
	baseURL     string
	accessToken string
	logger      log.Logger
}

// NewClient creates an analytics Client.
func NewClient(baseURL, accessToken string, logger log.Logger) (*Client, error) {
	httpClient := retryhttp.NewClient(logger)
	httpClient.RetryMax = maxHTTPClientRetries

	return &Client{
		httpClient:  httpClient,
		baseURL:     baseURL,
		accessToken: accessToken,
		logger:      logger,
	}, nil
}

// Put marshals payload as JSON and sends it via HTTP PUT to baseURL+path.
func (c *Client) Put(path string, payload any) error {
	requestURL := c.baseURL + path
	c.logger.Debugf("HTTP PUT: %s", requestURL)

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	c.logger.Debugf("Request body:\n%s", data)

	req, err := retryablehttp.NewRequest(http.MethodPut, requestURL, data)
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	c.logger.Debugf("Response: %d %s", resp.StatusCode, body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	return nil
}
