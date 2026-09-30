package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// SkipForEntitlement reports whether activation should stop before doing
// anything at all, because this workspace has no Build Cache. Stopping early is
// the point: activating anyway spends an analytics invocation to record a build
// that could never have used the cache.
//
// Lite never skips. Preboot has no workspace to ask about, so the question moves
// to build time along with everything else lite defers.
func SkipForEntitlement(ctx context.Context, logger log.Logger) bool {
	if Lite {
		return false
	}

	// Absence is not a "no": a machine with no credential yet is a different
	// case, handled by the activation itself with a better error than this.
	cred, _, found, err := live.Default(logger).ResolveAllowingNone(ctx, utils.AllEnvs(), true)
	if err != nil || !found {
		return false
	}

	return configcommon.SkipActivationForEntitlement(ctx, consts.BitriseWebsiteBaseURL, cred, logger)
}
