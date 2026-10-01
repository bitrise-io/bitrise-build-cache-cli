package activateall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

//nolint:gochecknoglobals
var autoMode bool

var ActivateAllCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "all",
	Short: "Activate Bitrise Build Cache for every supported tool",
	Long: `Activate Bitrise Build Cache for Gradle, Bazel, Xcode (macOS only) and C++ via ccache.

With --auto the activation was not asked for by the user, as when a platform starts it for every build.
It then runs only for workspaces listed in ` + configcommon.EnvAutoActivateOrgs + ` and does nothing for the rest.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		a := activator{
			logger:  logger,
			envs:    utils.AllEnvs(),
			goos:    runtime.GOOS,
			resolve: resolveCredential(logger),
			entitled: func(ctx context.Context, tools []string) bool {
				return !common.SkipForEntitlementOfAll(ctx, tools, logger)
			},
			runStep:   runSelf,
			autoGated: autoMode,
		}

		return a.activate(cmd.Context())
	},
}

type step struct {
	name string
	args []string
}

type activator struct {
	logger    log.Logger
	envs      map[string]string
	goos      string
	autoGated bool
	resolve   func(ctx context.Context) (cred auth.Credential, found bool, err error)
	entitled  func(ctx context.Context, tools []string) bool
	runStep   func(ctx context.Context, args []string) error
}

func (a activator) activate(ctx context.Context) error {
	if a.autoGated {
		if reason := a.autoSkipReason(ctx); reason != "" {
			a.logger.Infof("Bitrise Build Cache auto-activation skipped: %s", reason)

			return nil
		}
	}

	steps, tools := a.plan()
	if !a.entitled(ctx, tools) {
		return nil
	}

	var errs []error
	for _, s := range steps {
		a.logger.TInfof("Activating Bitrise Build Cache for %s", s.name)

		if err := a.runStep(ctx, s.args); err != nil {
			errs = append(errs, fmt.Errorf("activate %s: %w", s.name, err))
		}
	}

	return errors.Join(errs...)
}

// autoSkipReason fails closed: only a resolved credential on the list passes.
func (a activator) autoSkipReason(ctx context.Context) string {
	cred, found, err := a.resolve(ctx)
	if err != nil {
		return fmt.Sprintf("could not resolve the build's credential (%s)", err)
	}
	if !found {
		return "no credential in this build"
	}

	if !configcommon.OrgAllowedForAutoActivation(a.envs[configcommon.EnvAutoActivateOrgs], cred.WorkspaceID) {
		return fmt.Sprintf("workspace %q is not enabled for it", cred.WorkspaceID)
	}

	return ""
}

// plan returns the steps and their build tools; react-native runs cpp-only, the rest is covered above.
func (a activator) plan() ([]step, []string) {
	steps := []step{
		{"gradle", []string{"activate", "gradle"}},
		{"bazel", []string{"activate", "bazel"}},
	}
	tools := []string{configcommon.BuildToolGradle, configcommon.BuildToolBazel}

	if a.goos == "darwin" {
		steps = append(steps, step{"xcode", []string{"activate", "xcode"}})
		tools = append(tools, configcommon.BuildToolXcode)
	}

	steps = append(steps, step{"react-native", []string{"activate", "react-native", "--gradle=false", "--xcode=false"}})
	tools = append(tools, configcommon.BuildToolCpp)

	return steps, tools
}

func resolveCredential(logger log.Logger) func(context.Context) (auth.Credential, bool, error) {
	return func(ctx context.Context) (auth.Credential, bool, error) {
		cred, _, found, err := live.Default(logger).ResolveAllowingNone(ctx, utils.AllEnvs(), true)

		return cred, found, err //nolint:wrapcheck
	}
}

// runSelf isolates each tool in its own process, so one failing cannot stop the others.
func runSelf(ctx context.Context, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find the running executable: %w", err)
	}

	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	return cmd.Run() //nolint:wrapcheck
}

func init() {
	common.ActivateCmd.AddCommand(ActivateAllCmd)
	ActivateAllCmd.Flags().BoolVar(&autoMode, "auto", false,
		"The activation was started automatically for the build. Runs only for workspaces in "+configcommon.EnvAutoActivateOrgs+".")
}
