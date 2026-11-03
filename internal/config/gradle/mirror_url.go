package gradleconfig

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle/mirrors"
)

const (
	// MirrorEnabledEnvKey gates Bitrise-hosted Maven mirror usage. Must equal "true" to enable.
	MirrorEnabledEnvKey = "BITRISE_MAVENCENTRAL_PROXY_ENABLED"
	// MirrorDatacenterEnvKey identifies the datacenter the build runs in.
	MirrorDatacenterEnvKey = "BITRISE_DEN_VM_DATACENTER"

	mirrorURLPattern           = "https://repository-manager.services.bitrise.io:8090/maven/%s"
	mirrorSegmentGradlePlugins = "gradle-plugins"
)

// GradlePluginsMirrorURL returns the Bitrise-hosted gradle-plugins mirror URL
// (proxying https://plugins.gradle.org/m2/) for the current datacenter, or ""
// when the mirror is disabled, no datacenter is set (e.g. local dev), or the
// datacenter has no Bitrise mirror deployment (e.g. customer-private GCP envs
// like US_EAST4).
func GradlePluginsMirrorURL(envs map[string]string) string {
	if envs[MirrorEnabledEnvKey] != "true" {
		return ""
	}

	dc := envs[MirrorDatacenterEnvKey]
	if dc == "" {
		return ""
	}

	if !mirrors.IsSupportedDatacenter(dc) {
		return ""
	}

	return fmt.Sprintf(mirrorURLPattern, mirrorSegmentGradlePlugins)
}
