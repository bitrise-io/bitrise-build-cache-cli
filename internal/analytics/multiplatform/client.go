package multiplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
	"github.com/hashicorp/go-retryablehttp"
)

const maxHTTPClientRetries = 3

// The analytics service answers a failure to read the request body with 400 and
// this marker, so the request never reached the handler and a resend is safe.
// retryablehttp's default policy gives up on any 4xx, silently losing the
// invocation.
const requestBodyReadFailureMarker = "read request body"

// maxRetryProbeBodyBytes bounds how much of an error body the retry policy reads.
const maxRetryProbeBodyBytes = 4 << 10

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
	httpClient.CheckRetry = retryPolicy

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

	resp, doErr := c.httpClient.Do(req)
	if resp == nil {
		return fmt.Errorf("perform HTTP request: %w", doErr)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	c.logger.Debugf("Response: %d %s", resp.StatusCode, body)

	// Retries exhausted: keep the server's own body, it carries the diagnostic.
	if doErr != nil {
		return fmt.Errorf("HTTP %d: %s: %w", resp.StatusCode, body, doErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	return nil
}

func retryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if err == nil && resp != nil && resp.StatusCode == http.StatusBadRequest && isRequestBodyReadFailure(resp) {
		return true, nil
	}

	//nolint:wrapcheck // delegating to the library's own policy
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}

// isRequestBodyReadFailure reports whether a 400 is the service failing to read
// the request body. It rewinds resp.Body so the caller still sees it.
func isRequestBodyReadFailure(resp *http.Response) bool {
	if resp.Body == nil {
		return false
	}

	probe, err := io.ReadAll(io.LimitReader(resp.Body, maxRetryProbeBodyBytes))
	if err != nil && len(probe) == 0 {
		return false
	}

	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(probe), resp.Body), resp.Body}

	return strings.Contains(string(probe), requestBodyReadFailureMarker)
}
