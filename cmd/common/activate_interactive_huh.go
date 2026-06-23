package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/charmbracelet/huh"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

type huhWizard struct{}

func (*huhWizard) Run(ctx context.Context) error {
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
	logger.TInfof("Bitrise Build Cache - interactive local setup")

	kc := keychain.New()

	startWS, startToken, source := loadStartingCredentials(kc, os.Getenv(envWorkspaceID), os.Getenv(envAuthToken))

	var (
		selectedTools []string
		workspaceID   = startWS
		authToken     = startToken
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

	if source == credsSourceNone {
		groups = append(groups,
			huh.NewGroup(
				huh.NewInput().
					Title("Workspace ID").
					Description("Find it at https://app.bitrise.io").
					Validate(nonEmpty("Workspace ID")).
					Value(&workspaceID),
				huh.NewInput().
					Title("Auth token").
					Description("Personal access token. Input is hidden.").
					EchoMode(huh.EchoModePassword).
					Validate(nonEmpty("Auth token")).
					Value(&authToken),
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
	case credsSourceKeychain:
		logger.TInfof("Using credentials from the OS keychain.")
	case credsSourceEnv:
		if err := persistCredentials(kc, workspaceID, authToken); err != nil {
			logger.Warnf("Could not save credentials to the OS keychain (%v). Continuing with env values for this run only.", err)
		} else {
			logger.TInfof("Imported BITRISE_BUILD_CACHE_AUTH_TOKEN + WORKSPACE_ID from env into the OS keychain.")
			logger.Infof("You can now remove them from your shell rc files.")
		}
	case credsSourceNone:
		if err := persistCredentials(kc, workspaceID, authToken); err != nil {
			logger.Warnf("Could not save credentials to the OS keychain (%v). Continuing with values for this run only.", err)
		} else {
			logger.TInfof("Credentials saved to the OS keychain. Future runs will pick them up automatically.")
		}
	}

	envs := utils.AllEnvs()
	envs[envWorkspaceID] = workspaceID
	envs[envAuthToken] = authToken

	return runSelectedTools(ctx, logger, selectedTools, envs, pushEnabled)
}

func nonEmpty(label string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(label + " cannot be empty")
		}

		return nil
	}
}

type credsSource int

const (
	credsSourceNone credsSource = iota
	credsSourceKeychain
	credsSourceEnv
)

func loadStartingCredentials(kc keychainStore, envWS, envToken string) (string, string, credsSource) {
	if creds, err := kc.Load(); err == nil && creds.AuthToken != "" && creds.WorkspaceID != "" {
		return creds.WorkspaceID, creds.AuthToken, credsSourceKeychain
	}

	if envToken != "" && envWS != "" {
		return envWS, envToken, credsSourceEnv
	}

	return "", "", credsSourceNone
}

func persistCredentials(kc keychainStore, workspaceID, authToken string) error {
	if err := kc.Save(keychain.Credentials{AuthToken: authToken, WorkspaceID: workspaceID}); err != nil {
		return fmt.Errorf("save credentials to keychain: %w", err)
	}

	return nil
}
