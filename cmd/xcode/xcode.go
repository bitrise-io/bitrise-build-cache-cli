package xcode

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode/interactive"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/gitroot"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode/invoke"
)

//nolint:gochecknoglobals
var xcodeCommand = &cobra.Command{
	Use:   "xcode",
	Short: "Run xcodebuild build/test with Bitrise Build Cache.",
	Long: `Run xcodebuild through the Bitrise Build Cache wrapper for local development.

The build/test subcommands resolve the workspace / project / scheme / configuration /
destination from a repo-local config file (` + paths.BitriseBuildCacheDirRelative + `/xcode-{build,test}.json),
falling back to a DerivedData scan and finally an interactive prompt. The resolved
spec is persisted so subsequent runs reuse it without prompting.`,
}

func init() {
	common.RootCmd.AddCommand(xcodeCommand)
	xcodeCommand.AddCommand(newXcodeSubcommand(invoke.CommandBuild, "build", "Run `xcodebuild build` with Bitrise Build Cache."))
	xcodeCommand.AddCommand(newXcodeSubcommand(invoke.CommandTest, "test", "Run `xcodebuild test` with Bitrise Build Cache."))
}

func newXcodeSubcommand(command invoke.Command, use, short string) *cobra.Command {
	var (
		reconfigure bool
		codesign    bool
	)

	cmd := &cobra.Command{
		Use:                use,
		Short:              short,
		SilenceUsage:       true,
		DisableFlagParsing: false,
		RunE: func(cobraCmd *cobra.Command, positional []string) error {
			return runXcodeSubcommand(cobraCmd.Context(), cobraCmd, command, reconfigure, codesign, positional)
		},
	}

	cmd.Flags().BoolVar(&reconfigure, "reconfigure", false, "Bypass the cached invocation config and re-run discovery + prompt.")
	cmd.Flags().BoolVar(&codesign, "codesign", false, "Enable codesigning (default off for local runs).")

	return cmd
}

// resolveXcodeInvocation runs the full resolve → prompt → persist cycle and
// returns the final spec ready to feed into BuildArgv. Swappable for tests.
//
//nolint:gochecknoglobals
var resolveXcodeInvocation = func(ctx context.Context, command invoke.Command, repoRoot string, reconfigure bool) (invoke.InvocationSpec, error) {
	resolver := &invoke.Resolver{}

	if reconfigure {
		// Preflight to learn the config path, then wipe it so Resolve rediscovers.
		_, meta, err := resolver.Resolve(command, repoRoot)
		if err != nil {
			return invoke.InvocationSpec{}, err //nolint:wrapcheck // resolver errors are already contextual
		}

		if err := resolver.Reset(meta.ConfigPath); err != nil {
			return invoke.InvocationSpec{}, err //nolint:wrapcheck // resolver errors are already contextual
		}
	}

	spec, meta, err := resolver.Resolve(command, repoRoot)
	if err != nil {
		return invoke.InvocationSpec{}, err //nolint:wrapcheck // resolver errors are already contextual
	}

	if !spec.IsComplete() {
		spec, err = interactive.Prompter{}.Fill(ctx, spec, meta.ProjectDir)
		if err != nil {
			return invoke.InvocationSpec{}, err //nolint:wrapcheck // prompter errors are already contextual
		}
	}

	if err := resolver.Persist(meta.ConfigPath, spec); err != nil {
		return invoke.InvocationSpec{}, err //nolint:wrapcheck // resolver errors are already contextual
	}

	return spec, nil
}

func runXcodeSubcommand(ctx context.Context, cobraCmd *cobra.Command, command invoke.Command, reconfigure, codesign bool, positional []string) error {
	osProxy := utils.DefaultOsProxy{}

	cwd, err := osProxy.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	repoRoot, err := gitroot.Find(cwd, osProxy)
	switch {
	case errors.Is(err, gitroot.ErrNotInGitRepo):
		fmt.Fprintln(os.Stderr, "Warning: not inside a git repository — resolved invocation spec will not be persisted.")
		repoRoot = ""
	case err != nil:
		return fmt.Errorf("locate git repository: %w", err)
	}

	spec, err := resolveXcodeInvocation(ctx, command, repoRoot, reconfigure)
	if err != nil {
		if errors.Is(err, interactive.ErrPromptUnavailable) {
			return promptUnavailableError(command, err)
		}

		return err
	}

	argv := invoke.BuildArgv(spec, command, codesign)
	argv = append(argv, positional...)

	return runXcodebuildWrapperFn(ctx, argv, cobraCmd)
}

func promptUnavailableError(command invoke.Command, cause error) error {
	return fmt.Errorf("xcode %s: %w", commandName(command), cause)
}

func commandName(command invoke.Command) string {
	switch command {
	case invoke.CommandTest:
		return "test"
	case invoke.CommandBuild:
		fallthrough
	default:
		return "build"
	}
}
