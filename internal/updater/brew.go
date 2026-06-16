package updater

import (
	"github.com/bitrise-io/go-utils/v2/log"
)

// BrewFormula is the Homebrew formula name shipped by the Bitrise tap.
const BrewFormula = "bitrise-io/bitrise-build-cache/bitrise-build-cache"

// PrintBrewUpgrade emits the upgrade instruction for a brew-installed CLI.
// Running `brew upgrade` from the cellar-resident binary itself clashes with brew's file locking — let the user invoke it.
func PrintBrewUpgrade(logger log.Logger) {
	if logger == nil {
		return
	}

	logger.Infof("This CLI was installed via Homebrew. Run:")
	logger.Infof("  brew update && brew upgrade %s", BrewFormula)
	logger.Infof("to get the latest release.")
}
