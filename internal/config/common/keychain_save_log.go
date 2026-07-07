package common

import (
	"sync"

	"github.com/bitrise-io/go-utils/v2/log"
)

//nolint:gochecknoglobals
var keychainSaveLogOnce sync.Once

func LogKeychainSaved(logger log.Logger) {
	keychainSaveLogOnce.Do(func() {
		logger.Infof("Saved auth credentials to the OS keychain")
	})
}
