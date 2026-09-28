package bazelconfig

import (
	_ "embed"
	"strings"
)

//go:embed credhelper_shim.sh
var credHelperShimTemplate string

const credHelperVersionPlaceholder = "__BITRISE_BUILD_CACHE_VERSION__" //nolint:gosec // sentinel string, not a credential

// RenderCredHelperShim returns the shim script body with the version pin
// substituted. Pass "" or "latest" to leave the shim tracking the latest tag.
func RenderCredHelperShim(version string) string {
	return strings.Replace(credHelperShimTemplate, credHelperVersionPlaceholder, version, 1)
}

// CredHelperLineSnippet returns the .bazelrc snippet that points
// --credential_helper at the workspace-relative shim.
func CredHelperLineSnippet(shimRelPath string) string {
	return "build --credential_helper=*.services.bitrise.io=%workspace%/" + shimRelPath + "\n"
}
