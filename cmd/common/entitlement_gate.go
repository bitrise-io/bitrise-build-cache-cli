package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// SkipForEntitlement reports whether activation should stop before doing
// anything at all, because this workspace has no Build Cache. Stopping early
// avoids reporting an analytics invocation for a build that could never have
// used the cache.
func SkipForEntitlement(ctx context.Context, logger log.Logger) bool {
	// Absence is not a "no": a machine with no credential yet is a different
	// case, handled by the activation itself with a better error than this.
	cred, _, found, err := live.Default(logger).ResolveAllowingNone(ctx, utils.AllEnvs(), true)
	if err != nil || !found {
		return false
	}

	return SkipForEntitlementWith(ctx, logger, cred)
}

// SkipForEntitlementWith is SkipForEntitlement for a caller that already resolved the credential.
func SkipForEntitlementWith(ctx context.Context, logger log.Logger, cred auth.Credential) bool {
	app := configcommon.NewEntitlementApp(utils.AllEnvs(), utils.DefaultOsProxy{})

	return configcommon.SkipActivationForEntitlement(ctx, consts.BitriseWebsiteBaseURL, cred, app, logger)
}
