package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Skip where the credential is injected or brokered, which Resolve prefers over any stored login.
func hydrateStoredAuth(ctx context.Context) {
	envs := utils.AllEnvs()
	if envs[auth.EnvJWT] != "" || auth.OnBuildHub(envs) {
		return
	}
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
	// Resolve refreshes a store-managed credential as a side effect; a non-stored
	// one is a no-op, which is exactly the skip this used to hand-roll.
	if _, _, err := live.Default(logger).Resolve(ctx, envs); err != nil {
		logger.Debugf("OAuth login not applied: %s", err)
	}
}
