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
func SkipForEntitlement(ctx context.Context, buildTool string, logger log.Logger) bool {
	return SkipForEntitlementOfAll(ctx, []string{buildTool}, logger)
}

// SkipForEntitlementOfAll is the React Native case: one activation covering
// several tools. It stops only when every one of them is unentitled, because
// entitlement is granted per tool and a workspace with Gradle but not Xcode
// still has an activation worth doing.
func SkipForEntitlementOfAll(ctx context.Context, buildTools []string, logger log.Logger) bool {
	// Absence is not a "no": a machine with no credential yet is a different
	// case, handled by the activation itself with a better error than this.
	cred, _, found, err := live.Default(logger).ResolveAllowingNone(ctx, utils.AllEnvs(), true)
	if err != nil || !found {
		return false
	}

	for _, buildTool := range buildTools {
		if !configcommon.SkipActivationForEntitlement(ctx, buildTool, consts.BitriseWebsiteBaseURL, cred, logger) {
			return false
		}
	}

	return true
}
