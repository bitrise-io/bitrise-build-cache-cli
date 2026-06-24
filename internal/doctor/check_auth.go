package doctor

import (
	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

func (d *Doctor) authCheck() Check {
	return Check{
		Name: "auth",
		Diagnose: func(_ context.Context) Result {
			if d.AuthLoader != nil {
				if creds, err := d.AuthLoader.Load(); err == nil && creds.AuthToken != "" && creds.WorkspaceID != "" {
					return Result{State: StateOK, Detail: "OS keychain, workspace=" + creds.WorkspaceID}
				}
			}

			if ws, tok := d.Envs[common.EnvWorkspaceID], d.Envs[common.EnvAuthToken]; ws != "" && tok != "" {
				return Result{State: StateOK, Detail: "environment variables, workspace=" + ws}
			}

			if d.Envs[common.EnvJWT] != "" {
				return Result{State: StateOK, Detail: "CI JWT env (" + common.EnvJWT + ")"}
			}

			return Result{
				State:  StateError,
				Detail: "no credentials found. Run `bitrise-build-cache auth set --token … --workspace-id …` or `bitrise-build-cache activate --interactive`.",
			}
		},
	}
}
