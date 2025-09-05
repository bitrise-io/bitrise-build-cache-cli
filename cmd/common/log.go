package common

import (
	"os/user"

	"github.com/bitrise-io/go-utils/v2/log"
)

func LogCurrentUserInfo(logger log.Logger) {
	currentUser, err := user.Current()
	if err != nil {
		logger.Debugf("Error getting current user: %v", err)

		return
	}

	logger.Debugf("Current user info:")
	logger.Debugf("  UID: %s", currentUser.Uid)
	logger.Debugf("  GID: %s", currentUser.Gid)
	logger.Debugf("  Username: %s", currentUser.Username)
}
