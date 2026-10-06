// Package entitlementgate is the one place an activation asks whether the workspace has Build Cache.
package entitlementgate

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// SkipFor reports whether an activation should stop before writing anything, because this
// workspace has no Build Cache. It fails open: a missing credential, an unreachable website
// or an unexpected answer lets the activation continue.
func SkipFor(ctx context.Context, logger log.Logger) bool {
	return SkipForEnvs(ctx, logger, utils.AllEnvs())
}

// SkipForEnvs is SkipFor for a caller that carries its own environment.
func SkipForEnvs(ctx context.Context, logger log.Logger, envs map[string]string) bool {
	// Absence is not a "no": a machine with no credential yet is a different
	// case, handled by the activation itself with a better error than this.
	cred, _, found, err := live.Default(logger).ResolveAllowingNone(ctx, envs, true)
	if err != nil || !found {
		return false
	}

	return SkipForWith(ctx, logger, cred)
}

// SkipForWith is SkipFor for a caller that already resolved the credential.
func SkipForWith(ctx context.Context, logger log.Logger, cred auth.Credential) bool {
	return configcommon.SkipActivationForEntitlement(ctx, consts.BitriseWebsiteBaseURL, cred, logger)
}
