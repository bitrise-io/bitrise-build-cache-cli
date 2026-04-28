package gradleconfig

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	// MirrorEnabledEnvKey gates Bitrise-hosted Maven mirror usage. Must equal "true" to enable.
	MirrorEnabledEnvKey = "BITRISE_MAVENCENTRAL_PROXY_ENABLED"
	// MirrorDatacenterEnvKey identifies the datacenter, used to build the mirror region slug.
	MirrorDatacenterEnvKey = "BITRISE_DEN_VM_DATACENTER"

	mirrorURLPattern           = "https://repository-manager-%s.services.bitrise.io:8090/maven/%s"
	mirrorSegmentGradlePlugins = "gradle-plugins"
)

// GradlePluginsMirrorURL returns the Bitrise-hosted gradle-plugins mirror URL
// (proxying https://plugins.gradle.org/m2/) for the current datacenter, or ""
// when the mirror is disabled or no datacenter is set (e.g. local dev).
func GradlePluginsMirrorURL(envs map[string]string) string {
	if envs[MirrorEnabledEnvKey] != "true" {
		return ""
	}

	dc := envs[MirrorDatacenterEnvKey]
	if dc == "" {
		return ""
	}

	region := strings.TrimRightFunc(strings.ToLower(dc), unicode.IsDigit)

	return fmt.Sprintf(mirrorURLPattern, region, mirrorSegmentGradlePlugins)
}
