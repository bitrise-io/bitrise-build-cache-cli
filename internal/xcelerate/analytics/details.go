package analytics

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/go-retryablehttp"
)

const (
	invocationDetailsPath = "details"
	detailsAPIVersion     = "1"
	detailsSchemaVersion  = 1
)

// PutInvocationDetails wraps reportJSON in the versioned envelope while gzipping it,
// so only the compressed body is held (and replayed on retry).
func (c *Client) PutInvocationDetails(ctx context.Context, invocationID string, reportJSON io.Reader) error {
	requestURL := fmt.Sprintf("%s/invocations/%s/%s", c.baseURL, invocationID, invocationDetailsPath)
	c.logger.Debugf("Sending invocation details to: %s", requestURL)

	body, err := gzipDetailsEnvelope(reportJSON)
	if err != nil {
		return fmt.Errorf("encode invocation details: %w", err)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodPut, requestURL, body)
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

func gzipDetailsEnvelope(reportJSON io.Reader) ([]byte, error) {
	envelope := io.MultiReader(
		strings.NewReader(fmt.Sprintf(`{"schemaVersion":%d,"report":`, detailsSchemaVersion)),
		reportJSON,
		strings.NewReader("}"),
	)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)

	if _, err := io.Copy(zw, envelope); err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}

	return buf.Bytes(), nil
}
