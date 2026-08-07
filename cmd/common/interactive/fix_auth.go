package interactive

import (
	"context"
	"errors"
	"fmt"

	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/authprompt"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// FixAuthPrompt returns the credential collector the doctor's auth fixer uses:
// it offers the browser sign-in first and falls back to entering a token, so
// repairing auth is the same flow as the interactive setup rather than a second,
// token-only path.
func FixAuthPrompt(ctx context.Context, logger log.Logger) func() (string, string, error) {
	return func() (string, string, error) {
		useBrowser := true
		if err := tui.RunForm(huh.NewGroup(
			huh.NewConfirm().
				Title("How should the credentials be set?").
				Description("Signing in stores a login that refreshes itself; a token has to be replaced by hand when it expires.").
				Affirmative("Sign in with a browser").
				Negative("Enter a token").
				Value(&useBrowser),
		)); err != nil {
			return "", "", err //nolint:wrapcheck // tui.ErrAborted, or already wrapped
		}

		if !useBrowser {
			return authprompt.PromptAndSave() //nolint:wrapcheck // caller reports it
		}

		// No workspace flag to offer: `doctor --fix --interactive` takes none, so a
		// sign-in that leaves stdin unusable has to stop rather than point at one.
		out, err := loginAndStore(ctx, logger, utils.AllEnvs(), "", "", "")
		switch {
		case errors.Is(err, tui.ErrAborted), errors.Is(err, ErrStdinUnusable):
			return "", "", err //nolint:wrapcheck // sentinel
		case err != nil:
			logger.Warnf("Browser sign-in did not complete (%s).", err)
			logger.Infof("Falling back to entering a token.")

			return authprompt.PromptAndSave() //nolint:wrapcheck // caller reports it
		}

		if out.Creds.AuthToken == "" {
			return "", "", fmt.Errorf("sign-in returned no token")
		}

		return out.Creds.WorkspaceID, out.Creds.AuthToken, nil
	}
}
