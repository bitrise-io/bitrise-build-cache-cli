package bazelconfig

import (
	"context"
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// installerSnippet is the GitHub Actions-shaped install: writes the CLI to a
// PATH-persistent, non-transient dir (~/.local/bin) and pushes that dir onto
// $GITHUB_PATH so later steps see it. `/tmp/bin` and similar transient dirs
// disable the credential-helper branch in `activate` (see clibin.IsTransientPath),
// so the destination directory matters.
const installerSnippet = `mkdir -p "$HOME/.local/bin"
    curl -sfL https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh | sh -s -- -b "$HOME/.local/bin"
    echo "$HOME/.local/bin" >> "$GITHUB_PATH"`

// WarnIfHelperPinnedInRepo scans the repo-level bazelrc files Bazel would load
// and emits an actionable warning when one commits the Bitrise CLI as a
// --credential_helper. Non-fatal: scan errors are logged and swallowed so an
// unreadable file cannot break activation.
//
// cliOnPATH short-circuits the warning: the failure mode a pin causes only
// bites machines where `bitrise-build-cache` is missing from $PATH, so we do
// not need to warn a caller that already has it.
func WarnIfHelperPinnedInRepo(ctx context.Context, logger log.Logger, startDir string, osProxy utils.OsProxy, cliOnPATH bool) {
	if cliOnPATH {
		return
	}

	matches, err := ScanForPinnedHelper(ctx, startDir, osProxy, nil)
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
	b.WriteString("On GitHub Actions install it into a non-transient, on-PATH dir, e.g.\n")
	b.WriteString("  " + installerSnippet + "\n")
	b.WriteString("Do NOT install into /tmp, /var/folders, or similar — those are treated as transient and disable the credential-helper branch.\n")
	b.WriteString("Alternatively move the line into ~/.bazelrc (per-user, uncommitted) — " +
		"that is what `bitrise-build-cache activate bazel` already does.")

	return b.String()
}
