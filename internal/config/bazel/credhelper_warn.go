package bazelconfig

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const installerOneLiner = "curl -sfL https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh | sh -s -- -b /usr/local/bin"

// WarnIfHelperPinnedInRepo scans the repo-level bazelrc files Bazel would load
// and emits an actionable warning when one commits the Bitrise CLI as a
// --credential_helper. Non-fatal: scan errors are logged and swallowed so an
// unreadable file cannot break activation.
func WarnIfHelperPinnedInRepo(logger log.Logger, startDir string, osProxy utils.OsProxy) {
	matches, err := ScanForPinnedHelper(startDir, osProxy)
	if err != nil {
		logger.Debugf("bazel credhelper scan skipped: %s", err)

		return
	}
	if len(matches) == 0 {
		return
	}

	logger.Warnf("%s", PinnedHelperWarning(matches))
}

// PinnedHelperWarning renders the actionable warning message for matches
// returned by ScanForPinnedHelper. Kept public so doctor can render the same
// text.
func PinnedHelperWarning(matches []PinnedHelperMatch) string {
	var b strings.Builder
	b.WriteString("Detected a repo-level Bazel credential-helper pin for the Bitrise CLI:\n")
	for _, m := range matches {
		_, _ = fmt.Fprintf(&b, "  %s:%d: %s\n", m.Path, m.LineNumber, m.Line)
	}
	b.WriteString("Every machine running `bazel build` on this repo must have `bitrise-build-cache` on PATH.\n")
	b.WriteString("On CI runners (GitHub Actions in particular) install it e.g.\n")
	b.WriteString("  " + installerOneLiner + "\n")
	b.WriteString("Alternatively move the line into ~/.bazelrc (per-user, uncommitted) — " +
		"that is what `bitrise-build-cache activate bazel` already does.")

	return b.String()
}
