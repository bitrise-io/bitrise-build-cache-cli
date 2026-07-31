package doctor

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/authprompt"
)

type AuthPromptFixer struct {
	Prompt func() (workspaceID, authToken string, err error)
}

// NeedsTerminal marks this fixer as one that must ask the user something.
func (f AuthPromptFixer) NeedsTerminal() bool { return true }

func (f AuthPromptFixer) Fix() (string, error) {
	prompt := f.Prompt
	if prompt == nil {
		prompt = authprompt.PromptAndSave
	}

	workspaceID, _, err := prompt()
	if err != nil {
		return "", fmt.Errorf("auth prompt: %w", err)
	}

	// Deliberately no backend here: the prompt may have stored to the keychain or
	// to the config file (on CI, or when the keychain refuses the write), and this
	// signature can't tell which. `auth status` reports where it actually landed.
	return "saved credentials (workspace=" + workspaceID + ")", nil
}
