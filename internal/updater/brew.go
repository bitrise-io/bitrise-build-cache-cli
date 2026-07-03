package updater

import (
	"github.com/bitrise-io/go-utils/v2/log"
)

const BrewFormula = "bitrise-io/bitrise-build-cache/bitrise-build-cache"

// PrintBrewUpgrade prints rather than execs because brew upgrade run from inside the cellar binary clashes with brew's file locking.
func PrintBrewUpgrade(logger log.Logger) {
	if logger == nil {
		return
	}

	logger.Infof("This CLI was installed via Homebrew. Run:")
	logger.Infof("  brew update && brew upgrade %s", BrewFormula)
	logger.Infof("to get the latest release.")
}
