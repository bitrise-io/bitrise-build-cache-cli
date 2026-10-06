package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/entitlementgate"
)

// SkipForEntitlement reports whether activation should stop before doing anything at all,
// because this workspace has no Build Cache.
func SkipForEntitlement(ctx context.Context, logger log.Logger) bool {
	return entitlementgate.SkipFor(ctx, logger)
}

// SkipForEntitlementWith is SkipForEntitlement for a caller that already resolved the credential.
func SkipForEntitlementWith(ctx context.Context, logger log.Logger, cred auth.Credential) bool {
	return entitlementgate.SkipForWith(ctx, logger, cred)
}
