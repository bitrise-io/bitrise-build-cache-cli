package bazelconfig

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"text/template"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

var (
	errAuthTokenNotProvided   = errors.New("AuthToken not provided")
	errEndpointURLNotProvided = errors.New("EndpointURL not provided")
)

//go:embed bazelrc.gotemplate
var bazelrcTemplateText string

type templateInventory struct {
	EndpointURL string
	WorkspaceID string
	AuthToken   string
	// Metadata
	CacheConfigMetadata common.CacheConfigMetadata
}

// Generate bazelrc.
func GenerateBazelrc(endpointURL, workspaceID, authToken string, cacheConfigMetadata common.CacheConfigMetadata) (string, error) {
	// required check
	if len(authToken) < 1 {
		return "", fmt.Errorf("generate bazelrc, error: %w", errAuthTokenNotProvided)
	}

	if len(endpointURL) < 1 {
		return "", fmt.Errorf("generate bazelrc, error: %w", errEndpointURLNotProvided)
	}

	// create inventory
	inventory := templateInventory{
		EndpointURL: endpointURL,
		WorkspaceID: workspaceID,
		AuthToken:   authToken,
		// Metadata
		CacheConfigMetadata: cacheConfigMetadata,
	}

	tmpl, err := template.New("bazelrc").Parse(bazelrcTemplateText)
	if err != nil {
		return "", fmt.Errorf("generate bazelrc: invalid template: %w", err)
	}

	resultBuffer := bytes.Buffer{}
	if err = tmpl.Execute(&resultBuffer, inventory); err != nil {
		return "", fmt.Errorf("GenerateBazelrc: %w", err)
	}

	return resultBuffer.String(), nil
}
