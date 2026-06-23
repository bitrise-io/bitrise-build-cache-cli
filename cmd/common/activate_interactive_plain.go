package common

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

type plainWizard struct {
	prompter *prompter
}

func (w *plainWizard) Run(ctx context.Context) error {
	p := w.prompter
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
	logger.TInfof("Bitrise Build Cache - interactive local setup")

	fmt.Fprintln(p.out)
	fmt.Fprintln(p.out, "This wizard will configure Bitrise Build Cache for a build tool on this machine.")
	fmt.Fprintln(p.out, "You can find your workspace ID and a personal access token at: https://app.bitrise.io")
	fmt.Fprintln(p.out)

	tool, err := promptTool(p)
	if err != nil {
		return err
	}

	workspaceID, authToken, err := resolveCredentials(p, keychain.New())
	if err != nil {
		return err
	}

	pushEnabled, err := promptPushEnabled(p)
	if err != nil {
		return err
	}

	envs := utils.AllEnvs()
	envs[envWorkspaceID] = workspaceID
	envs[envAuthToken] = authToken

	switch tool {
	case toolGradle:
		return runInteractiveGradle(logger, envs, pushEnabled)
	case toolBazel:
		return runInteractiveBazel(logger, envs, pushEnabled)
	case toolXcode:
		return runInteractiveXcode(ctx, logger, envs, pushEnabled)
	case toolCcache:
		return runInteractiveCcache(ctx, logger, envs, pushEnabled)
	default:
		return fmt.Errorf("unsupported tool: %s", tool)
	}
}
