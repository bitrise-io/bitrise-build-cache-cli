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
	CacheEndpointURL    string
	RBEEndpointURL      string
	WorkspaceID         string
	AuthToken           string
	IsTimestampsEnabled bool
	// CacheConfigMetadata
	CacheConfigMetadata common.CacheConfigMetadata
}

type Preferences struct {
	RBEEndpointURL      string
	IsTimestampsEnabled bool
}

// Generate bazelrc.
func GenerateBazelrc(cacheEndpointURL, workspaceID, authToken string,
	cacheConfigMetadata common.CacheConfigMetadata,
	prefs Preferences) (string, error) {
	// required check
	if len(authToken) < 1 {
		return "", fmt.Errorf("generate bazelrc, error: %w", errAuthTokenNotProvided)
	}

	if len(cacheEndpointURL) < 1 {
		return "", fmt.Errorf("generate bazelrc, error: %w", errEndpointURLNotProvided)
	}

	if len(prefs.RBEEndpointURL) < 1 {
		prefs.RBEEndpointURL = ""
	}

	// create inventory
	inventory := templateInventory{
		CacheEndpointURL:    cacheEndpointURL,
		RBEEndpointURL:      prefs.RBEEndpointURL,
		WorkspaceID:         workspaceID,
		AuthToken:           authToken,
		IsTimestampsEnabled: prefs.IsTimestampsEnabled,
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
