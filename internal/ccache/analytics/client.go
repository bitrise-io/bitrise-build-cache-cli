package analytics

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics/multiplatform"
)

// Client sends ccache analytics to the Bitrise backend.
// It embeds multiplatform.Client for shared PutInvocation and PutInvocationRelation methods.
type Client struct {
	*multiplatform.Client
}

// NewClient creates an analytics Client.
func NewClient(baseURL, accessToken string, logger log.Logger) (*Client, error) {
	mp, err := multiplatform.NewClient(baseURL, accessToken, logger)
	if err != nil {
		return nil, fmt.Errorf("create multiplatform client: %w", err)
	}

	return &Client{Client: mp}, nil
}
