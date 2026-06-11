package common

import (
	"context"
	"os"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/versioncheck"
)

// NoUpdateCheck is set by the global --no-update-check flag. Read from the
// root PersistentPreRun to gate the GitHub release lookup. Exported so other
// subcommands can short-circuit additional network calls if needed.
//
//nolint:gochecknoglobals
var NoUpdateCheck bool

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

		// Version-drift detection (ACI-5037). Best-effort: never block the
		// invocation. Subcommands that don't represent a user-facing action
		// (help, completion, the daemon up/down/restart imperatives that just
		// poke launchctl) skip the check to keep their output deterministic.
		if shouldSkipVersionCheck(cmd) {
			return
		}

		runVersionCheck(cmd)
	},
}

// shouldSkipVersionCheck returns true for command names that shouldn't trigger
// a version drift check / GitHub network lookup. Keep this list tight — the
// check is supposed to fire on every real CLI invocation.
func shouldSkipVersionCheck(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "version", "help", "completion":
		return true
	default:
		return false
	}
}

// runVersionCheck performs the drift detect + nudge with a generous context
// timeout (so a hung GitHub call can't slow a CI / dev run). Errors are
// swallowed — the version check is advisory.
func runVersionCheck(cmd *cobra.Command) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer cancel()

	_, _ = versioncheck.RunOnce(ctx, versioncheck.Options{
		CurrentVersion: configcommon.GetCLIVersion(log.NewLogger()),
		Home:           home,
		NoUpdateCheck:  NoUpdateCheck,
		Out:            os.Stderr,
		IsCI:           isRunningOnCI(),
	})
}

// isRunningOnCI is the heuristic we use to suppress the nudge on CI. Matches
// what most CI providers set: CI=true (Bitrise, GitHub Actions, CircleCI),
// or the presence of a Bitrise-specific env var.
func isRunningOnCI() bool {
	if v, ok := os.LookupEnv("CI"); ok && v != "" && v != "0" && v != "false" {
		return true
	}

	if _, ok := os.LookupEnv("BITRISE_BUILD_NUMBER"); ok {
		return true
	}

	return false
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
	RootCmd.PersistentFlags().BoolVar(&NoUpdateCheck, "no-update-check", false,
		"Skip the GitHub release lookup that nudges when a newer CLI version is available")
}
