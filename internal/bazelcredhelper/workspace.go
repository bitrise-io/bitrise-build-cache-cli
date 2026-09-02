package bazelcredhelper

import (
	"fmt"
	"io"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// discoverFromCWD is the Bazel-specific entry point: Bazel invokes the
// credential helper with CWD = workspace root, so the marker walk-up starts
// from there.
func discoverFromCWD(osProxy utils.OsProxy, warn io.Writer) string {
	cwd, err := osProxy.Getwd()
	if err != nil {
		if warn != nil {
			_, _ = fmt.Fprintf(warn, "bitrise-build-cache: cannot resolve CWD, using machine-wide credential: %s\n", err)
		}

		return ""
	}

	return configcommon.DiscoverWorkspaceSlug(cwd, osProxy, warn)
}
