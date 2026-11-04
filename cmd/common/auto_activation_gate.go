package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// AutoActivationEnabled reports whether this app and workflow may be activated
// automatically, for a caller that already resolved the credential.
func AutoActivationEnabled(ctx context.Context, logger log.Logger, cred auth.Credential) bool {
	envs := utils.AllEnvs()
	app := configcommon.NewAppIdentity(envs, utils.DefaultOsProxy{})

	return configcommon.AutoActivationChecker(ctx, configcommon.SelectWebsiteBaseURL(envs), cred, app, logger)
}
