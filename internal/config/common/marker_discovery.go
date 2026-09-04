package common

import (
	"fmt"
	"io"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// DiscoverWorkspaceSlug walks up from startDir looking for a project marker
// and returns the workspace slug. An empty return means "no per-project scope"
// — either no marker up to the filesystem root or the marker could not be
// parsed. Read/parse errors are warned so an operator can spot a broken marker,
// then fall back to the machine-wide credential rather than fail the build.
func DiscoverWorkspaceSlug(startDir string, osProxy utils.OsProxy, warn io.Writer) string {
	if startDir == "" {
		return ""
	}

	_, marker, err := WalkUpFindMarker(startDir, osProxy)
	if err != nil {
		if warn != nil {
			_, _ = fmt.Fprintf(warn, "bitrise-build-cache: reading project marker failed, using machine-wide credential: %s\n", err)
		}

		return ""
	}
	if marker == nil {
		return ""
	}

	return marker.Workspace
}
