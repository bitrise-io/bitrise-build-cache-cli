package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Skip where Resolve would not reach a stored login, or would get past it to an
// exchange: minting a credential here would cost one on every command, including
// ones that never authenticate.
func hydrateStoredAuth(ctx context.Context) {
	envs := utils.AllEnvs()
	if envs[auth.EnvJWT] != "" || auth.OnGitHubActionsOIDC(envs) {
		return
	}
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
	resolver := live.Default(logger)
	if resolver.BrokeringEnv(envs) != "" {
		return
	}
	// Resolve refreshes a store-managed credential as a side effect; a non-stored
	// one is a no-op, which is exactly the skip this used to hand-roll.
	if _, _, err := resolver.Resolve(ctx, envs); err != nil {
		logger.Debugf("OAuth login not applied: %s", err)
	}
}
