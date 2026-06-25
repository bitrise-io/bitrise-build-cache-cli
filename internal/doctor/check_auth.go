package doctor

import (
	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

func (d *Doctor) authCheck() Check {
	return Check{
		Name: "auth",
		Diagnose: func(_ context.Context) Result {
			// Selection stays keychain-first (nudge users toward the keychain);
			// the description comes from config common's shared layer, so the
			// OAuth-login + expiry detail matches `status` and `auth status`.
			if d.AuthLoader != nil {
				if cfg, ok := common.GetKeychainCredentialsWith(d.AuthLoader); ok {
					desc := common.DescribeResolvedWith(cfg, common.AuthSourceKeychain, d.AuthLoader)

					return Result{State: StateOK, Detail: desc.Detail()}
				}
			}

			if ws, tok := d.Envs[common.EnvWorkspaceID], d.Envs[common.EnvAuthToken]; ws != "" && tok != "" {
				desc := common.DescribeResolved(common.CacheAuthConfig{AuthToken: tok, WorkspaceID: ws}, common.AuthSourceEnvVars)

				return Result{State: StateOK, Detail: desc.Detail()}
			}

			if d.Envs[common.EnvJWT] != "" {
				desc := common.DescribeResolved(common.CacheAuthConfig{}, common.AuthSourceJWT)

				return Result{State: StateOK, Detail: desc.Detail()}
			}

			return Result{
				State:  StateError,
				Detail: "no credentials found. Run `bitrise-build-cache auth set --token … --workspace-id …` or `bitrise-build-cache activate --interactive`.",
			}
		},
	}
}
