package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// AutoActivationEnabled reports whether this app and workflow may be activated
// automatically, for a caller that already resolved the credential.
func AutoActivationEnabled(ctx context.Context, logger log.Logger, cred auth.Credential) bool {
	app := configcommon.NewAppIdentity(utils.AllEnvs(), utils.DefaultOsProxy{})

	return configcommon.AutoActivationChecker(ctx, consts.BitriseWebsiteBaseURL, cred, app, logger)
}
