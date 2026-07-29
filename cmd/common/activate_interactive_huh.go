package common

import (
	"context"
	"errors"
	"fmt"

	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"

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

	// Resolved before the form: the sign-in prints logs, opens a browser and
	// reads stdin, none of which composes with huh's full-screen form.
	auth := wizardAuthResolver{
		Logger:   logger,
		Keychain: kc,
		Envs:     envs,
		Prompt:   wizardPromptReader(),
	}.Resolve(ctx)
	storedCreds := auth.Stored
	source := auth.Source
	storedUsername := storedCreds.Username

	var (
		selectedTools []string
		workspaceID   = auth.Config.WorkspaceID
		authToken     = auth.Config.AuthToken
		username      = storedUsername
		pushEnabled   bool
		startDaemon   = true
	)

	// Tool selection is its own form: huh's accessible mode (TERM=dumb) ignores
	// group hide funcs, so the daemon question below can only be conditional if
	// the group doesn't exist yet when the tools are unknown.
	if err := runForm(huh.NewGroup(
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
	)); err != nil {
		return err
	}

	var groups []*huh.Group

	if auth.NeedsManualPrompt() {
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

	needsDaemon := len(daemonServicesForTools(selectedTools)) > 0
	if needsDaemon {
		groups = append(groups,
			huh.NewGroup(
				huh.NewConfirm().
					Title("Keep the cache proxies running in the background?").
					Description("Registers them with the OS supervisor (LaunchAgents on macOS, systemd --user on Linux) and starts them, so you don't have to run them per terminal session.").
					Affirmative("Yes, install + start").
					Negative("No, I'll start them myself").
					Value(&startDaemon),
			),
		)
	}

	if err := runForm(groups...); err != nil {
		return err
	}

	persistWizardCredentials(logger, kc, auth, wizardCredentials{
		WorkspaceID:    workspaceID,
		AuthToken:      authToken,
		Username:       username,
		StoredUsername: storedUsername,
	})

	envs[configcommon.EnvWorkspaceID] = workspaceID
	envs[configcommon.EnvAuthToken] = authToken

	if err := runSelectedTools(ctx, logger, selectedTools, envs, pushEnabled); err != nil {
		return err
	}

	if needsDaemon && startDaemon {
		startDaemonForTools(ctx, logger, selectedTools)
	}

	return nil
}

// wizardCredentials are the credential values the form ended up with.
type wizardCredentials struct {
	WorkspaceID    string
	AuthToken      string
	Username       string
	StoredUsername string
}

// persistWizardCredentials saves the wizard's credentials to the keychain, with
// a message describing what moved where. A failure is non-fatal: activation can
// still proceed with the values resolved for this run.
func persistWizardCredentials(logger log.Logger, kc keychainStore, auth wizardAuth, creds wizardCredentials) {
	save := func() error {
		return persistCredentials(kc, auth.Stored, creds.WorkspaceID, creds.AuthToken, creds.Username)
	}

	logUsername := func() {
		if creds.Username != "" {
			logger.Infof("Saved display name %q for local invocations.", creds.Username)
		}
	}

	switch auth.Source {
	case configcommon.AuthSourceKeychain:
		if !auth.SignedInNow {
			logger.TInfof("Using credentials from the OS keychain.")
		}
		if creds.Username == creds.StoredUsername {
			return
		}
		if err := save(); err != nil {
			logger.Warnf("Could not update the OS keychain with the new display name (%v).", err)
		} else {
			logger.Infof("Updated display name for local invocations.")
		}
	case configcommon.AuthSourceEnvVars:
		if err := save(); err != nil {
			logger.Warnf("Could not save credentials to the OS keychain (%v). Continuing with env values for this run only.", err)
		} else {
			logger.TInfof("Imported BITRISE_BUILD_CACHE_AUTH_TOKEN + WORKSPACE_ID from env into the OS keychain.")
			logUsername()
			logger.Infof("You can now remove them from your shell rc files.")
		}
	case configcommon.AuthSourceJWT:
		// Per-build, don't persist.
		logger.TInfof("Using credentials resolved by the CLI.")
	case configcommon.AuthSourceMultiplatform, configcommon.AuthSourceFile:
		if err := save(); err != nil {
			logger.Warnf("Could not save credentials to the OS keychain (%v). Continuing with disk values for this run only.", err)
		} else {
			logger.TInfof("Migrated credentials from the config file to the OS keychain.")
			logUsername()
		}
	case configcommon.AuthSourceNone:
		if err := save(); err != nil {
			logger.Warnf("Could not save credentials to the OS keychain (%v). Continuing with values for this run only.", err)
		} else {
			logger.TInfof("Credentials saved to the OS keychain. Future runs will pick them up automatically.")
		}
	}
}

func runForm(groups ...*huh.Group) error {
	if err := huh.NewForm(groups...).Run(); err != nil {
		return fmt.Errorf("interactive wizard: %w", err)
	}

	return nil
}

// wizardStartingCreds enforces keychain-first precedence for the wizard:
// keychain wins over env vars (so a populated keychain isn't silently overridden
// by stale shell-rc env vars), then we fall back to ResolveAuthConfig for the
// env / JWT / multiplatform sources, returning AuthSourceNone if none are set.
func wizardStartingCreds(
	envs map[string]string,
	storedCreds keychain.Credentials,
	resolve func(map[string]string) (configcommon.CacheAuthConfig, configcommon.AuthSource, error),
) (configcommon.CacheAuthConfig, configcommon.AuthSource) {
	if storedCreds.AuthToken != "" && storedCreds.WorkspaceID != "" {
		return configcommon.CacheAuthConfig{AuthToken: storedCreds.AuthToken, WorkspaceID: storedCreds.WorkspaceID}, configcommon.AuthSourceKeychain
	}

	if resolve == nil {
		resolve = configcommon.ResolveAuthConfig
	}

	cfg, src, err := resolve(envs)
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
