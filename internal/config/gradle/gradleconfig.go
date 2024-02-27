package gradleconfig

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"text/template"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
)

//go:embed initd.gradle.kts.gotemplate
var gradleTemplateText string

var (
	errAuthTokenNotProvided   = errors.New("AuthToken not provided")
	errEndpointURLNotProvided = errors.New("EndpointURL not provided")
)

type templateInventory struct {
	AuthToken                string
	CacheEndpointURLWithPort string
	CachePluginVersion       string
	PushEnabled              bool
	DebugEnabled             bool
	ValidationLevel          string
	AnalyticsEnabled         bool
	AnalyticsPluginVersion   string
	AnalyticsEndpoint        string
	AnalyticsPort            int
	AnalyticsHTTPEndpoint    string
	// Metadata
	CacheConfigMetadata common.CacheConfigMetadata
}

// Generate init.gradle content.
// Recommended to save the content into $HOME/.gradle/init.d/ instead of
// overwriting the $HOME/.gradle/init.gradle file.
func GenerateInitGradle(endpointURL, authToken string, analyticsEnabled bool, cacheConfigMetadata common.CacheConfigMetadata) (string, error) {
	// required check
	if len(authToken) < 1 {
		return "", fmt.Errorf("generate init.gradle, error: %w", errAuthTokenNotProvided)
	}

	if len(endpointURL) < 1 {
		return "", fmt.Errorf("generate init.gradle, error: %w", errEndpointURLNotProvided)
	}

	// create inventory
	inventory := templateInventory{
		AuthToken:                authToken,
		CacheEndpointURLWithPort: endpointURL,
		CachePluginVersion:       consts.GradleRemoteBuildCachePluginDepVersion,
		PushEnabled:              true,
		DebugEnabled:             true,
		ValidationLevel:          "warning",
		AnalyticsEnabled:         analyticsEnabled,
		AnalyticsPluginVersion:   consts.GradleAnalyticsPluginDepVersion,
		AnalyticsEndpoint:        consts.GradleAnalyticsEndpoint,
		AnalyticsPort:            consts.GradleAnalyticsPort,
		AnalyticsHTTPEndpoint:    consts.GradleAnalyticsHTTPEndpoint,
		CacheConfigMetadata:      cacheConfigMetadata,
	}

	tmpl, err := template.New("init.gradle").Parse(gradleTemplateText)
	if err != nil {
		return "", fmt.Errorf("generate init.gradle: invalid template: %w", err)
	}

	resultBuffer := bytes.Buffer{}
	if err = tmpl.Execute(&resultBuffer, inventory); err != nil {
		return "", fmt.Errorf("GenerateInitGradle: %w", err)
	}

	return resultBuffer.String(), nil
}
