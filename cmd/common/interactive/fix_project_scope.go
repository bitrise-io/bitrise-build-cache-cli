package interactive

import (
	"context"

	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
)

// ProjectScopePromptFn confirms whether to drop the opt-in marker at dir.
type ProjectScopePromptFn = func(dir string) (bool, error)

// FixProjectScopePrompt returns the confirm the doctor's project-scope fixer uses,
// so the opt-in form is the same whether it fires from `activate` or from `doctor`.
// ctx + logger are accepted to match the sibling FixAuthPrompt / PickWorkspacePrompt
// shape even though this prompt neither cancels nor logs.
func FixProjectScopePrompt(_ context.Context, _ log.Logger) ProjectScopePromptFn {
	return func(cwd string) (bool, error) {
		confirmed := false
		if err := tui.RunForm(huh.NewGroup(
			huh.NewConfirm().
				Title("No marker found at " + cwd + ". Create .bitrise-build-cache.json here?").
				Description("Without a marker this directory stays outside opt-in scope and the cache won't activate for builds launched here.").
				Affirmative("Yes, opt this project in").
				Negative("No, I'll do it later").
				Value(&confirmed),
		)); err != nil {
			return false, err //nolint:wrapcheck // caller logs it
		}

		return confirmed, nil
	}
}
