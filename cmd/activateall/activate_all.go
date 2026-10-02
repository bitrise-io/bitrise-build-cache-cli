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
It then runs only for workspaces listed in ` + configcommon.EnvAutoActivateOrgs + ` (or when that is "all" or "*", for every
workspace that has a credential) and does nothing for the rest.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		a := activator{
			logger:  logger,
			envs:    utils.AllEnvs(),
			goos:    runtime.GOOS,
			resolve: resolveCredential(logger),
			entitled: func(ctx context.Context, cred auth.Credential) bool {
				return !common.SkipForEntitlementWith(ctx, logger, cred)
			},
			runStep:   runSelf,
			autoGated: autoMode,
			debug:     common.IsDebugLogMode,
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
	debug     bool
	resolve   func(ctx context.Context) (cred auth.Credential, found bool, err error)
	entitled  func(ctx context.Context, cred auth.Credential) bool
	runStep   func(ctx context.Context, args []string) error
}

func (a activator) activate(ctx context.Context) error {
	cred, found, err := a.resolve(ctx)

	if a.autoGated {
		if reason := a.autoSkipReason(cred, found, err); reason != "" {
			a.logger.Infof("Bitrise Build Cache auto-activation skipped: %s", reason)

			return nil
		}
	}

	// Absence is not a "no": the activations report a missing credential better than this gate.
	if err == nil && found && !a.entitled(ctx, cred) {
		return nil
	}

	var errs []error
	for _, s := range a.plan() {
		a.logger.TInfof("Activating Bitrise Build Cache for %s", s.name)

		if err := a.runStep(ctx, s.args); err != nil {
			errs = append(errs, fmt.Errorf("activate %s: %w", s.name, err))
		}
	}

	return errors.Join(errs...)
}

// autoSkipReason fails closed: only a resolved credential on the list passes.
func (a activator) autoSkipReason(cred auth.Credential, found bool, err error) string {
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

// plan returns the steps; react-native runs cpp-only, the rest is covered above.
func (a activator) plan() []step {
	steps := []step{
		{"gradle", []string{"activate", "gradle"}},
		{"bazel", []string{"activate", "bazel"}},
	}

	if a.goos == "darwin" {
		steps = append(steps, step{"xcode", []string{"activate", "xcode"}})
	}

	steps = append(steps, step{"react-native", []string{"activate", "react-native", "--gradle=false", "--xcode=false"}})

	if a.debug {
		for i := range steps {
			steps[i].args = append(steps[i].args, "-d")
		}
	}

	return steps
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
