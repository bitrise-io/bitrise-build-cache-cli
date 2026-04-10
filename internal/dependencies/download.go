package dependencies

import (
	"fmt"
	"io"
	"net/http"
)

func downloadFile(url string) (io.ReadCloser, error) {
	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return nil, fmt.Errorf("HTTP GET: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return resp.Body, nil
}
