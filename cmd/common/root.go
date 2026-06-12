package common

import (
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

// rootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:     "bitrise-build-cache-cli",
	Version: configcommon.GetCLIVersion(log.NewLogger()),
	Short:   "Bitrise Build Cache CLI - to enable/configure Gradle or Bazel build cache on the machine where you run this CLI.",
	Long: `Bitrise Build Cache CLI - to enable/configure Gradle or Bazel build cache on the machine where you run this CLI.

What does the CLI do on a high level?

It creates the necessary config to enable Build Cache and Command Exec/Invocation Analytics. It does this via adding the config in the $HOME directory.

In case of Gradle it's done via creating or modifying the following two files: $HOME/.gradle/init.d/bitrise-build-cache.init.gradle and $HOME/.gradle/gradle.properties (adding org.gradle.caching=true to gradle.properties).

In case of Bazel it's done via creating or modifying $HOME/.bazelrc.`,
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		// `version` already prints the CLI version itself; skip the duplicate log line.
		if cmd.Name() == "version" {
			return
		}

		configcommon.LogCLIVersion(log.NewLogger(log.WithDebugLog(IsDebugLogMode)))

		// For cache commands, make a prior `bitrise-build-cache login` take
		// effect: refresh the stored OAuth PAT and export it as the auth env
		// vars when none are already set. No-op for login/logout themselves,
		// and never overrides manual/CI credentials.
		switch cmd.Name() {
		case "login", "logout", "completion", "help", "status":
			// status is read-only and reports the auth source itself, so it
			// must not trigger a refresh that would also mask the source.
		default:
			hydrateStoredAuth(cmd.Context())
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		if code, ok := HandleStatusExit(err); ok {
			os.Exit(code)
		}

		os.Exit(1)
	}
}

func init() {
	RootCmd.PersistentFlags().BoolVarP(&IsDebugLogMode, "debug", "d", false, "Enable debug logging mode")
}
