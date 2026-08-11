package doctor

import (
	"errors"
	"fmt"
)

// WorkspacePickFixer completes a sign-in that stopped before the workspace step.
type WorkspacePickFixer struct {
	Prompt func() (workspaceID string, err error)
}

func (f WorkspacePickFixer) NeedsTerminal() bool { return true }

func (f WorkspacePickFixer) Fix() (string, error) {
	if f.Prompt == nil {
		return "", errors.New("no workspace picker available: run `bitrise-build-cache auth workspace --list`, then `auth workspace --set <slug>`")
	}

	workspaceID, err := f.Prompt()
	if err != nil {
		return "", fmt.Errorf("workspace picker: %w", err)
	}

	return "selected workspace " + workspaceID, nil
}
