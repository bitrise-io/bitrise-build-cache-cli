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

	mirrorURLPattern    = "https://repository-manager-%s.services.bitrise.io:8090/maven/%s"
	mirrorSegmentApache = "apache-central"
)

// ApacheCentralMirrorURL returns the Bitrise-hosted apache-central mirror URL
// for the current datacenter, or "" when the mirror is disabled or no datacenter
// is set (e.g. local dev).
func ApacheCentralMirrorURL(envs map[string]string) string {
	if envs[MirrorEnabledEnvKey] != "true" {
		return ""
	}

	dc := envs[MirrorDatacenterEnvKey]
	if dc == "" {
		return ""
	}

	region := strings.TrimRightFunc(strings.ToLower(dc), unicode.IsDigit)

	return fmt.Sprintf(mirrorURLPattern, region, mirrorSegmentApache)
}
