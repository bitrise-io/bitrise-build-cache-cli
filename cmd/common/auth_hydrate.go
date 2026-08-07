package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Skip on Bitrise CI where JWT is env-injected; self-hosted CI with a stored PAT still refreshes.
func hydrateStoredAuth(ctx context.Context) {
	envs := utils.AllEnvs()
	if envs[configcommon.EnvJWT] != "" {
		return
	}
	_, source, _ := configcommon.ResolveAuthConfig(envs)
	if !source.StoreManaged() {
		return
	}
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
	cfg := oauth.NewConfigFromEnv(utils.AllEnvs())
	cfg.Logger = logger
	if _, err := cfg.EnsureFresh(ctx); err != nil {
		logger.Debugf("OAuth login not applied: %s", err)
	}
}
