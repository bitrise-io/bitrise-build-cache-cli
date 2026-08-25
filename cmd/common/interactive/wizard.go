package interactive

import (
	"context"
	"errors"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/authprompt"
	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type huhWizard struct{}

func (*huhWizard) Run(ctx context.Context) error {
	logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
	logger.TInfof("Bitrise Build Cache - interactive local setup")

	credStore := store.NewKeychain()
	envs := utils.AllEnvs()

	// Resolved before the form: the sign-in prints logs, opens a browser and
	// reads stdin, none of which composes with huh's full-screen form.
	auth := wizardAuthResolver{
		Logger:    logger,
		Store:     credStore,
		Envs:      envs,
		Prompt:    wizardPromptReader(),
		Workspace: interactiveWorkspace,
	}.Resolve(ctx)

	// A sign-in that took over stdin makes every form below unreliable, and the
	// credential is already saved — so stop and let a second run, which finds that
	// credential and skips the sign-in, do the setup with a clean stdin.
	if auth.StdinUnusable {
		logger.Println()
		logger.TInfof("Credentials are saved, but the sign-in left standard input unusable for prompts.")
		logger.Infof("Run `bitrise-build-cache activate --interactive` again to finish — it won't sign in a second time.")

		return nil
	}

	storedCreds := auth.Stored
	origin := auth.Origin
	storedUsername := storedCreds.Username

	var (
		selectedTools = defaultSelectedTools()
		workspaceID   = auth.Config.WorkspaceID
		authToken     = auth.Config.Token
		username      = storedUsername
		pushEnabled   bool
		startDaemon   = true
	)

	toolOptions := []huh.Option[string]{
		huh.NewOption("Gradle", string(toolGradle)),
		huh.NewOption("Bazel", string(toolBazel)),
		huh.NewOption("Xcode", string(toolXcode)),
		huh.NewOption("ccache (C/C++)", string(toolCcache)),
	}

	toolsDescription := "Use space to toggle, enter to confirm."
	if note := resolvedAuthNote(auth); note != "" {
		toolsDescription += "\n\n" + note
	}

	// Tool selection is its own form: huh's accessible mode (TERM=dumb) ignores
	// group hide funcs, so the daemon question below can only be conditional if
	// the group doesn't exist yet when the tools are unknown.
	if err := tui.RunForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Which build tools should I set up?").
			Description(toolsDescription).
			Options(toolOptions...).
			Height(len(toolOptions) + tui.Chrome(toolsDescription)).
			Validate(func(s []string) error {
				if len(s) == 0 {
					return errors.New("pick at least one tool")
				}

				return nil
			}).
			Value(&selectedTools),
	)); err != nil {
		return err //nolint:wrapcheck // tui.ErrAborted, or an already-wrapped huh error
	}

	var groups []*huh.Group

	if auth.NeedsManualPrompt() {
		groups = append(groups, authprompt.Group(&workspaceID, &authToken))
	}

	if usernamePersistable(origin) {
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

	if err := tui.RunForm(groups...); err != nil {
		return err //nolint:wrapcheck // tui.ErrAborted, or an already-wrapped huh error
	}

	persistWizardCredentials(logger, credStore, auth, wizardCredentials{
		WorkspaceID:    workspaceID,
		AuthToken:      authToken,
		Username:       username,
		StoredUsername: storedUsername,
	})

	envs[authpkg.EnvWorkspaceID] = workspaceID
	envs[authpkg.EnvAuthToken] = authToken

	// Wizard's own daemon prompt is authoritative — suppress sub-activators' Ensure.
	if err := os.Setenv(daemonpkg.EnvSkipEnsure, "1"); err != nil {
		logger.Debugf("Could not set %s: %v", daemonpkg.EnvSkipEnsure, err)
	}

	envs[daemonpkg.EnvSkipEnsure] = "1"

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

type saveWithFallbackFn func(target store.Store, creds authpkg.TokenSet, allowFallback bool) (store.SaveResult, error)

func persistWizardCredentials(logger log.Logger, target store.Store, auth wizardAuth, creds wizardCredentials) {
	persistWizardCredentialsTo(logger, target, store.SaveExclusiveWithFallback, auth, creds)
}

func persistWizardCredentialsTo(
	logger log.Logger,
	target store.Store,
	saveFn saveWithFallbackFn,
	auth wizardAuth,
	creds wizardCredentials,
) {
	merged := auth.Stored
	merged.AuthToken = creds.AuthToken
	merged.WorkspaceID = creds.WorkspaceID
	merged.Username = creds.Username

	logUsername := func() {
		if creds.Username != "" {
			logger.Infof("Saved display name %q for local invocations.", creds.Username)
		}
	}

	if auth.SignedInNow {
		// loginAndStore already persisted this credential, in auth.Origin — which is
		// the config file on a host with no usable keychain. Only a changed display
		// name is left to write, and it goes to that same backend.
		if creds.Username == creds.StoredUsername {
			return
		}
		if err := persistCredentials(storeFor(target, auth.Origin.Backend), auth.Stored, creds.WorkspaceID, creds.AuthToken, creds.Username); err != nil {
			logger.Warnf("Could not save the display name to the %s (%v).", auth.Origin.Label(), err)
		} else {
			logger.Infof("Updated display name for local invocations.")
		}

		return
	}

	switch auth.Origin.Backend {
	case authpkg.BackendKeychain:
		logger.TInfof("Using credentials from the OS keychain.")
		if creds.Username == creds.StoredUsername {
			return
		}
		if err := target.Save(merged); err != nil {
			logger.Warnf("Could not update the OS keychain with the new display name (%v).", err)
		} else {
			logger.Infof("Updated display name for local invocations.")
		}
	case authpkg.BackendEnv:
		result, err := saveFn(target, merged, true)
		if err != nil {
			logger.Warnf("Could not save credentials to the %s (%v). Continuing with env values for this run only.", result.Origin.Label(), err)

			return
		}
		result.WarnFallback(logger)
		logger.TInfof("Imported BITRISE_BUILD_CACHE_AUTH_TOKEN + WORKSPACE_ID from env into the %s.", result.Origin.Label())
		logUsername()
		logger.Infof("You can now remove them from your shell rc files.")
	case authpkg.BackendJWT:
		// Per-build, don't persist.
		logger.TInfof("Using credentials resolved by the CLI.")
	case authpkg.BackendFile:
		result, err := saveFn(target, merged, true)
		if err != nil {
			logger.Warnf("Could not save credentials to the %s (%v). Continuing with disk values for this run only.", result.Origin.Label(), err)

			return
		}
		switch result.Origin.Backend {
		case authpkg.BackendFile:
			logger.Warnf("Keychain unavailable (%v). Credentials stay in the config file.", result.KeychainErr)
		case authpkg.BackendNone, authpkg.BackendEnv, authpkg.BackendJWT, authpkg.BackendKeychain:
			logger.TInfof("Moved credentials from the config file into the OS keychain.")
		}
		logUsername()
	case authpkg.BackendNone:
		result, err := saveFn(target, merged, true)
		if err != nil {
			logger.Warnf("Could not save credentials to the %s (%v). Continuing with values for this run only.", result.Origin.Label(), err)

			return
		}
		result.WarnFallback(logger)
		logger.TInfof("Credentials saved to the %s. Future runs will pick them up automatically.", result.Origin.Label())
		logUsername()
	}
}

// usernamePersistable reports whether a display name can be stored for this
// credential. Everything except a CI JWT qualifies: the JWT is minted per build,
// so there is nothing durable to attach a name to.
func usernamePersistable(origin authpkg.Origin) bool {
	return origin.Backend != authpkg.BackendJWT
}

func persistCredentials(kc store.Store, existing authpkg.TokenSet, workspaceID, authToken, username string) error {
	existing.AuthToken = authToken
	existing.WorkspaceID = workspaceID
	existing.Username = username
	if err := kc.Save(existing); err != nil {
		return fmt.Errorf("save credentials to keychain: %w", err)
	}

	return nil
}

// storeFor picks the backend to write to, keeping the injected store so tests stay
// off the real one.
func storeFor(target store.Store, backend authpkg.Backend) store.Store {
	if backend == authpkg.BackendFile {
		return store.NewFile()
	}

	return target
}
