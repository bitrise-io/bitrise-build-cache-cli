package authprompt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
)

func Group(workspaceID, authToken *string) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Workspace ID").
			Description("Find it at https://app.bitrise.io").
			Validate(nonEmpty("Workspace ID")).
			Value(workspaceID),
		huh.NewInput().
			Title("Auth token").
			Description("Personal access token. Input is hidden.").
			EchoMode(huh.EchoModePassword).
			Validate(nonEmpty("Auth token")).
			Value(authToken),
	)
}

type KeychainSaver interface {
	Save(creds keychain.Credentials) error
}

type Prompter struct {
	Keychain KeychainSaver
	RunForm  func(*huh.Group) error
}

//nolint:nonamedreturns // multiple string returns benefit from labels at call sites
func (p Prompter) PromptAndSave() (workspaceID, authToken string, err error) {
	runForm := p.RunForm
	if runForm == nil {
		runForm = defaultRunForm
	}

	if err := runForm(Group(&workspaceID, &authToken)); err != nil {
		return "", "", fmt.Errorf("auth prompt: %w", err)
	}

	workspaceID = strings.TrimSpace(workspaceID)
	authToken = strings.TrimSpace(authToken)

	kc := p.Keychain
	if kc == nil {
		kc = keychain.New()
	}

	if err := kc.Save(keychain.Credentials{AuthToken: authToken, WorkspaceID: workspaceID}); err != nil {
		return workspaceID, authToken, fmt.Errorf("save credentials to keychain: %w", err)
	}

	return workspaceID, authToken, nil
}

//nolint:nonamedreturns // mirrors Prompter.PromptAndSave
func PromptAndSave() (workspaceID, authToken string, err error) {
	return Prompter{}.PromptAndSave()
}

func defaultRunForm(group *huh.Group) error {
	if err := huh.NewForm(group).Run(); err != nil {
		return fmt.Errorf("huh form: %w", err)
	}

	return nil
}

func nonEmpty(label string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(label + " cannot be empty")
		}

		return nil
	}
}
