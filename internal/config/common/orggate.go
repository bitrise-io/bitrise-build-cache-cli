package common

import (
	"strings"
	"unicode"
)

// EnvAutoActivateOrgs lists the workspaces an automatic activation may run for:
// slugs separated by commas or whitespace, or "all" (or "*") for every workspace.
const EnvAutoActivateOrgs = "BITRISE_BUILD_CACHE_AUTO_ACTIVATE_ORGS"

const (
	allOrgs      = "all"
	allOrgsAlias = "*"
)

// OrgAllowedForAutoActivation fails closed: no list, no workspace, or a
// workspace that is not on the list all deny.
func OrgAllowedForAutoActivation(allowlist, workspaceID string) bool {
	if workspaceID == "" {
		return false
	}

	entries := strings.FieldsFunc(allowlist, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	for _, entry := range entries {
		if strings.EqualFold(entry, allOrgs) || entry == allOrgsAlias || strings.EqualFold(entry, workspaceID) {
			return true
		}
	}

	return false
}
