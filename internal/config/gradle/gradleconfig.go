package gradleconfig

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"text/template"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
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
	CacheVersion             string
	PushEnabled              bool
	DebugEnabled             bool
	ValidationLevel          string
	MetricsEnabled           bool
	MetricsVersion           string
	MetricsEndpoint          string
	MetricsPort              int
	CacheConfigMetadata      common.CacheConfigMetadata
}

// Sync the major version of this step and the library.
// Use the latest 1.x version of our dependency, so we don't have to update this definition after every lib release.
// But don't forget to update this to `2.+` if the library reaches version 2.0!
const gradleRemoteBuildCachePluginDepVersion = "1.+"

// Sync the major version of this step and the plugin.
// Use the latest 1.x version of the plugin, so we don't have to update this definition after every plugin release.
// But don't forget to update this to `2.+` if the library reaches version 2.0!
const analyticsPluginDepVersion = "0.+"
const analyticsEndpoint = "gradle-analytics.services.bitrise.io"
const analyticsPort = 443

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
		CacheVersion:             gradleRemoteBuildCachePluginDepVersion,
		PushEnabled:              true,
		DebugEnabled:             true,
		ValidationLevel:          "warning",
		MetricsEnabled:           analyticsEnabled,
		MetricsVersion:           analyticsPluginDepVersion,
		MetricsEndpoint:          analyticsEndpoint,
		MetricsPort:              analyticsPort,
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
