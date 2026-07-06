package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
)

//nolint:gochecknoglobals
var interactiveFlag bool

type interactiveTool string

const (
	toolGradle interactiveTool = "gradle"
	toolBazel  interactiveTool = "bazel"
	toolXcode  interactiveTool = "xcode"
	toolCcache interactiveTool = "ccache"
)

func init() { //nolint:gochecknoinits
	ActivateCmd.Flags().BoolVar(&interactiveFlag, "interactive", false,
		"Launch an interactive guided local setup. Prompts for the tool and credentials instead of reading them from environment variables.")
	ActivateCmd.SilenceUsage = true
	ActivateCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if !interactiveFlag {
			return cmd.Help() //nolint:wrapcheck // help has no useful error to wrap
		}

		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New(`interactive setup requires a terminal. For scripted use:
  bitrise-build-cache auth set --token <token> --workspace-id <workspace-id>
  bitrise-build-cache activate gradle   # or bazel / xcode / c++`)
		}

		return (&huhWizard{}).Run(cmd.Context())
	}
}

type keychainStore interface {
	Load() (keychain.Credentials, error)
	Save(creds keychain.Credentials) error
}

func runSelectedTools(ctx context.Context, logger log.Logger, tools []string, envs map[string]string, pushEnabled bool) error {
	for _, t := range tools {
		var err error
		switch interactiveTool(t) {
		case toolGradle:
			err = runInteractiveGradle(logger, envs, pushEnabled)
		case toolBazel:
			err = runInteractiveBazel(logger, envs, pushEnabled)
		case toolXcode:
			err = runInteractiveXcode(ctx, logger, envs, pushEnabled)
		case toolCcache:
			err = runInteractiveCcache(ctx, logger, envs, pushEnabled)
		default:
			err = fmt.Errorf("unsupported tool: %s", t)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func runInteractiveGradle(logger log.Logger, envs map[string]string, pushEnabled bool) error {
	gradleHome, err := pathutil.NewPathModifier().AbsPath("~/.gradle")
	if err != nil {
		return fmt.Errorf("expand Gradle home path: %w", err)
	}

	params := gradleconfig.DefaultActivateGradleParams()
	params.Cache.Enabled = true
	params.Cache.PushEnabled = pushEnabled

	if cliPath, exeErr := os.Executable(); exeErr == nil {
		params.CLIPath = cliPath
	}

	if err := gradleconfig.Activate(
		logger,
		gradleHome,
		envs,
		IsDebugLogMode,
		params.TemplateInventory,
		func(inventory gradleconfig.TemplateInventory, path string) error {
			return inventory.WriteToGradleInit(
				logger,
				path,
				utils.DefaultOsProxy{},
				gradleconfig.GradleTemplateProxy(),
			)
		},
		gradleconfig.DefaultGradlePropertiesUpdater(),
		params,
	); err != nil {
		return fmt.Errorf("activate plugins for Gradle: %w", err)
	}

	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		if err := gradleconfig.WriteSidecar(home, gradleconfig.Sidecar{
			InitScriptPath:   paths.FromHome(home).GradleInitScriptFile(),
			CacheEnabled:     params.Cache.Enabled,
			CachePushEnabled: params.Cache.PushEnabled,
			AnalyticsEnabled: params.Analytics.Enabled,
		}); err != nil {
			logger.Debugf("gradle sidecar write failed (non-fatal): %s", err)
		}
	}

	logger.TInfof("✅ Bitrise Build Cache activated for Gradle")

	return nil
}

func runInteractiveBazel(logger log.Logger, envs map[string]string, pushEnabled bool) error {
	homeDir, err := pathutil.NewPathModifier().AbsPath("~")
	if err != nil {
		return fmt.Errorf("expand home path: %w", err)
	}

	bazelrcPath := filepath.Join(homeDir, ".bazelrc")
	params := bazelconfig.DefaultActivateBazelParams()
	params.Cache.PushEnabled = pushEnabled

	commandFunc := func(cmd string, args ...string) (string, error) {
		out, err2 := exec.Command(cmd, args...).CombinedOutput() //nolint:noctx
		if err2 == nil {
			return string(out), nil
		}

		return string(out), fmt.Errorf("run cmd: %w", err2)
	}

	inventory, err := params.TemplateInventory(logger, envs, commandFunc, IsDebugLogMode)
	if err != nil {
		return fmt.Errorf("build Bazel template inventory: %w", err)
	}

	if err := inventory.WriteToBazelrc(logger, bazelrcPath, utils.DefaultOsProxy{}, utils.DefaultTemplateProxy()); err != nil {
		return fmt.Errorf("write .bazelrc: %w", err)
	}

	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		if mErr := bazelconfig.WriteSidecar(home, bazelconfig.Sidecar{
			BazelrcPath:       bazelrcPath,
			CacheEnabled:      params.Cache.Enabled,
			CachePushEnabled:  params.Cache.PushEnabled,
			BESEnabled:        params.BES.Enabled,
			RBEEnabled:        params.RBE.Enabled,
			TimestampsEnabled: params.Timestamps,
		}); mErr != nil {
			logger.Debugf("bazel sidecar write failed (non-fatal): %s", mErr)
		}
	}

	logger.TInfof("✅ Bitrise Build Cache activated for Bazel")

	return nil
}

func runInteractiveCcache(ctx context.Context, logger log.Logger, envs map[string]string, pushEnabled bool) error {
	activator := ccachepkg.NewActivator(ccachepkg.ActivatorParams{
		PushEnabled:  pushEnabled,
		DebugLogging: IsDebugLogMode,
		Envs:         envs,
		Logger:       logger,
	})

	if err := activator.Activate(ctx); err != nil {
		return fmt.Errorf("activate Bitrise Build Cache for ccache: %w", err)
	}

	return nil
}

func runInteractiveXcode(ctx context.Context, logger log.Logger, envs map[string]string, pushEnabled bool) error {
	params := xcelerate.DefaultParams()
	params.DebugLogging = IsDebugLogMode
	params.PushEnabled = pushEnabled

	if err := xcelerate.Activate(
		ctx,
		logger,
		utils.DefaultOsProxy{},
		utils.DefaultCommandFunc(),
		utils.DefaultEncoderFactory{},
		utils.DefaultDecoderFactory{},
		params,
		envs,
	); err != nil {
		return fmt.Errorf("activate Bitrise Build Cache for Xcode: %w", err)
	}

	logger.TInfof("✅ Bitrise Build Cache activated for Xcode")

	return nil
}
