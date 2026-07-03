package doctor

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/authprompt"
)

type AuthPromptFixer struct {
	Prompt func() (workspaceID, authToken string, err error)
}

func (f AuthPromptFixer) Fix() (string, error) {
	prompt := f.Prompt
	if prompt == nil {
		prompt = authprompt.PromptAndSave
	}

	workspaceID, _, err := prompt()
	if err != nil {
		return "", fmt.Errorf("auth prompt: %w", err)
	}

	return "saved credentials to OS keychain (workspace=" + workspaceID + ")", nil
}
