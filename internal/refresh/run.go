package refresh

import (
	"github.com/bitrise-io/go-utils/v2/log"
)

func OnBump(logger log.Logger, home, previousVersion, currentVersion string) error {
	reg, err := Load(home)
	if err != nil {
		return err
	}

	Notify(logger, previousVersion, currentVersion, reg.SortedEntries())

	return nil
}
