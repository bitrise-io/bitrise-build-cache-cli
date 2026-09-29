package bazelconfig

import (
	"os"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// LiteMarker is the comment a lite activation renders into the generated
// bazelrc block. It lives in the bazelrc itself, not in a file beside it, so the
// marker and the config it describes are written together and cannot disagree —
// a bazelrc that configures the credential helper always carries the answer to
// "was there a credential when this was written?".
const LiteMarker = "# bitrise-build-cache: activated at VM warmup (lite)"

// IsLiteBazelrc reports whether the home bazelrc was written by a lite run.
// False on any error: a file we cannot prove was warmed up keeps the loud
// missing-credential message, which is the one a misconfigured developer needs.
func IsLiteBazelrc(home string) bool {
	body, err := os.ReadFile(paths.FromHome(home).BazelrcFile()) //nolint:gosec // path derived from home + constant
	if err != nil {
		return false
	}

	for line := range strings.SplitSeq(string(body), "\n") {
		if strings.TrimSpace(line) == LiteMarker {
			return true
		}
	}

	return false
}
