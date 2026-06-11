package updater

import (
	"fmt"
	"io"
)

// BrewFormula is the Homebrew formula name shipped by the Bitrise tap. Kept
// as a constant in one place so this and any future docs / nudge text stay
// consistent.
const BrewFormula = "bitrise-io/tools/bitrise-build-cache-cli"

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
