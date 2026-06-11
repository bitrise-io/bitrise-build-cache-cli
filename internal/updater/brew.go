package updater

import (
	"fmt"
	"io"
)

// BrewFormula is the Homebrew formula name shipped by the Bitrise tap.
// Resolves to the formula at
// github.com/bitrise-io/homebrew-bitrise-build-cache/Formula/bitrise-build-cache.rb
// (see scripts/publish_homebrew_formula.sh: TAP_REMOTE_DEFAULT +
// the rendered Formula/bitrise-build-cache.rb path). Kept as a single
// constant so this string and the docs / nudge text never drift.
const BrewFormula = "bitrise-io/bitrise-build-cache/bitrise-build-cache"

// PrintBrewUpgrade writes the upgrade instruction for a brew-installed CLI
// to w. We deliberately don't *run* the upgrade ourselves — invoking
// `brew upgrade` from a binary that lives inside the cellar can clash with
// brew's own file locking. Letting the user run it from their shell avoids
// that whole class of issues.
func PrintBrewUpgrade(w io.Writer) {
	_, _ = fmt.Fprintf(w,
		"This CLI was installed via Homebrew. Run:\n  brew update && brew upgrade %s\nto get the latest release.\n",
		BrewFormula,
	)
}
