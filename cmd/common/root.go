package common

import (
	"context"
	"os"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/refresh"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/versioncheck"
)

//nolint:gochecknoglobals
var NoUpdateCheck bool

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
		if cmd.Name() == "version" {
			return
		}

		configcommon.LogCLIVersion(log.NewLogger(log.WithDebugLog(IsDebugLogMode)))

		// Apply a stored OAuth login: refresh its PAT into the auth env vars
		// (no-op when env/CI creds are set). Before the version check so it
		// still runs for check-skipped commands.
		switch cmd.Name() {
		case "login", "logout", "completion", "help", "status":
			// status reports the auth source, so it must not refresh and mask it.
		default:
			hydrateStoredAuth(cmd.Context())
		}

		if ShouldSkipVersionCheck(cmd) {
			return
		}

		RunVersionCheck(cmd)
	},
}

func ShouldSkipVersionCheck(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "version", "help", "completion", "update":
		return true
	case
		"xcodebuild",
		"start-proxy", "stop-proxy",
		"start", "stop", "set-invocation-id", "health-check", "collect-stats",
		"register-invocation", "register-child-invocation":
		return true
	case "token":
		return true
	default:
		return false
	}
}

// newVersionCheckLogger pins the version-check output to stderr; go-utils default is stdout.
func newVersionCheckLogger() log.Logger {
	return log.NewLogger(
		log.WithDebugLog(IsDebugLogMode),
		log.WithOutput(os.Stderr),
	)
}

func RunVersionCheck(cmd *cobra.Command) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer cancel()

	logger := newVersionCheckLogger()

	res, _ := versioncheck.RunOnce(ctx, versioncheck.Options{
		CurrentVersion: configcommon.GetCLIVersion(logger),
		Home:           home,
		NoUpdateCheck:  NoUpdateCheck,
		Logger:         logger,
		IsCI:           configcommon.DetectCIProvider(utils.AllEnvs()) != "",
	})

	if res.Drift.Kind == versioncheck.Bump {
		refresh.OnBump(logger, home)
	}
}

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
		"Suppress the new-release nudge")
}
