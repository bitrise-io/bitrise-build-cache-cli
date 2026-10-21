package common

import (
	"github.com/bitrise-io/go-utils/v2/log"
)

// PrintNextSteps uses plain Printf/Println — TInfof would prefix each line with
// a timestamp and fragment the banner.
func PrintNextSteps(logger log.Logger, bullets []string, docsURL string) {
	const border = "────────────────────────────────────────────────────────────"

	logger.Println()
	logger.Printf(border)
	logger.Printf("Next steps")
	logger.Printf(border)

	for i, bullet := range bullets {
		logger.Printf(" %d. %s", i+1, bullet)
	}

	if docsURL != "" {
		logger.Println()
		logger.Printf(" See %s for details.", docsURL)
	}

	logger.Printf(border)
	logger.Println()
}
