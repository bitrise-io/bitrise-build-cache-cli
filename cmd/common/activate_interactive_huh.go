package common

import (
	"context"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/charmbracelet/huh"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/authprompt"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type huhWizard struct{}

func (*huhWizard) Run(ctx context.Context) error {
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
	logger.TInfof("Bitrise Build Cache - interactive local setup")

	kc := keychain.New()
	envs := utils.AllEnvs()

	storedCreds, loadErr := kc.Load()
	switch {
	case loadErr == nil, errors.Is(loadErr, keychain.ErrNotFound):
	default:
		logger.Warnf("Could not read the OS keychain (%v). Wizard treats it as empty.", loadErr)
		storedCreds = keychain.Credentials{}
	}

	stored, source := wizardStartingCreds(envs, storedCreds)
	storedUsername := storedCreds.Username

	var (
		selectedTools []string
		workspaceID   = stored.WorkspaceID
		authToken     = stored.AuthToken
		username      = storedUsername
		pushEnabled   bool
	)

	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Which build tools should I set up?").
				Description("Use space to toggle, enter to confirm.").
				Options(
					huh.NewOption("Gradle", string(toolGradle)),
					huh.NewOption("Bazel", string(toolBazel)),
					huh.NewOption("Xcode", string(toolXcode)),
					huh.NewOption("ccache (C/C++)", string(toolCcache)),
				).
				Validate(func(s []string) error {
					if len(s) == 0 {
						return errors.New("pick at least one tool")
					}

					return nil
				}).
				Value(&selectedTools),
		),
	}

	if source == configcommon.AuthSourceNone {
		groups = append(groups, authprompt.Group(&workspaceID, &authToken))
	}

	if usernamePersistable(source) {
		groups = append(groups,
			huh.NewGroup(
				huh.NewInput().
					Title("Display name for this machine's local invocations").
					Description("Used to tag your local invocations in the Bitrise Build Cache dashboard. Leave empty to clear any stored override and fall back to the OS username.").
					Value(&username),
			),
		)
	}

	groups = append(groups,
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable cache push?").
				Description("Default off — recommended for local dev (so a flaky local build can't poison the shared cache).").
				Affirmative("Yes, push too").
				Negative("No, pull only").
				Value(&pushEnabled),
		),
	)

	if err := huh.NewForm(groups...).Run(); err != nil {
		return fmt.Errorf("interactive wizard: %w", err)
	}

	switch source {
	case configcommon.AuthSourceKeychain:
		logger.TInfof("Using credentials from the OS keychain.")
		if username != storedUsername {
			if err := persistCredentials(kc, storedCreds, workspaceID, authToken, username); err != nil {
				logger.Warnf("Could not update the OS keychain with the new display name (%v).", err)
			} else {
				logger.Infof("Updated display name for local invocations.")
			}
		}
	case configcommon.AuthSourceEnvVars:
		if err := persistCredentials(kc, storedCreds, workspaceID, authToken, username); err != nil {
			logger.Warnf("Could not save credentials to the OS keychain (%v). Continuing with env values for this run only.", err)
		} else {
			logger.TInfof("Imported BITRISE_BUILD_CACHE_AUTH_TOKEN + WORKSPACE_ID from env into the OS keychain.")
			if username != "" {
				logger.Infof("Saved display name %q for local invocations.", username)
			}
			logger.Infof("You can now remove them from your shell rc files.")
		}
	case configcommon.AuthSourceJWT:
		// Per-build, don't persist.
		logger.TInfof("Using credentials resolved by the CLI.")
	case configcommon.AuthSourceMultiplatform, configcommon.AuthSourceFile:
		if err := persistCredentials(kc, storedCreds, workspaceID, authToken, username); err != nil {
			logger.Warnf("Could not save credentials to the OS keychain (%v). Continuing with disk values for this run only.", err)
		} else {
			logger.TInfof("Migrated credentials from the config file to the OS keychain.")
			if username != "" {
				logger.Infof("Saved display name %q for local invocations.", username)
			}
		}
	case configcommon.AuthSourceNone:
		if err := persistCredentials(kc, storedCreds, workspaceID, authToken, username); err != nil {
			logger.Warnf("Could not save credentials to the OS keychain (%v). Continuing with values for this run only.", err)
		} else {
			logger.TInfof("Credentials saved to the OS keychain. Future runs will pick them up automatically.")
		}
	}

	envs[configcommon.EnvWorkspaceID] = workspaceID
	envs[configcommon.EnvAuthToken] = authToken

	return runSelectedTools(ctx, logger, selectedTools, envs, pushEnabled)
}

// wizardStartingCreds enforces keychain-first precedence for the wizard:
// keychain wins over env vars (so a populated keychain isn't silently overridden
// by stale shell-rc env vars), then we fall back to ResolveAuthConfig for the
// env / JWT / multiplatform sources, returning AuthSourceNone if none are set.
func wizardStartingCreds(envs map[string]string, storedCreds keychain.Credentials) (configcommon.CacheAuthConfig, configcommon.AuthSource) {
	if storedCreds.AuthToken != "" && storedCreds.WorkspaceID != "" {
		return configcommon.CacheAuthConfig{AuthToken: storedCreds.AuthToken, WorkspaceID: storedCreds.WorkspaceID}, configcommon.AuthSourceKeychain
	}

	cfg, src, err := configcommon.ResolveAuthConfig(envs)
	if err != nil {
		return configcommon.CacheAuthConfig{}, configcommon.AuthSourceNone
	}

	return cfg, src
}

func usernamePersistable(source configcommon.AuthSource) bool {
	return source == configcommon.AuthSourceKeychain ||
		source == configcommon.AuthSourceEnvVars ||
		source == configcommon.AuthSourceMultiplatform ||
		source == configcommon.AuthSourceFile ||
		source == configcommon.AuthSourceNone
}

func persistCredentials(kc keychainStore, existing keychain.Credentials, workspaceID, authToken, username string) error {
	existing.AuthToken = authToken
	existing.WorkspaceID = workspaceID
	existing.Username = username
	if err := kc.Save(existing); err != nil {
		return fmt.Errorf("save credentials to keychain: %w", err)
	}

	return nil
}
