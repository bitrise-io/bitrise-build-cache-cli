package analytics

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"
)

const (
	invocationDetailsPath = "details"
	detailsAPIVersion     = "1"
)

// PutInvocationDetails gzips the already-encoded JSON body so the client stays
// agnostic of the details payload shape.
func (c *Client) PutInvocationDetails(ctx context.Context, invocationID string, body []byte) error {
	requestURL := fmt.Sprintf("%s/invocations/%s/%s", c.baseURL, invocationID, invocationDetailsPath)
	c.logger.Debugf("Sending invocation details to: %s", requestURL)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		return fmt.Errorf("gzip invocation details: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("gzip invocation details: %w", err)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodPut, requestURL, buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.tokenSupplier()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Version", detailsAPIVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return unwrapError(resp)
	}

	return nil
}
