package common

import (
	"fmt"
	"io"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Empty return means no per-project scope. Read/parse errors warn but never
// fail — a broken marker falls back to the machine-wide credential.
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
