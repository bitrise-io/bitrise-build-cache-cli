package update

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/updater"
)

//nolint:gochecknoglobals
var dryRunFlag bool

//nolint:gochecknoglobals
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Upgrade the Bitrise Build Cache CLI binary in place",
	Long: `update detects how this CLI was installed (Homebrew vs manual installer) and runs the corresponding upgrade flow:

- Homebrew install: prints the exact ` + "`brew upgrade ...`" + ` command to run. We don't invoke brew from inside the Cellar to avoid lock clashes.
- Manual install (` + "`installer.sh`" + ` to a custom bindir): re-downloads installer.sh and runs it against the same bindir, replacing the binary in place.

After a successful manual upgrade, prints a hint to restart the daemon (` + "`bitrise-build-cache daemon restart`" + `) when one is installed.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve running executable path: %w", err)
		}

		method := updater.DetectInstallMethod(exe)
		logger.Infof("Detected install method: %s (binary at %s)", method, exe)

		switch method {
		case updater.InstallBrew:
			updater.PrintBrewUpgrade(logger)
		case updater.InstallManual:
			bindir := updater.BindirOf(exe)
			if _, err := updater.ManualUpgrade(cmd.Context(), updater.ManualOptions{
				Bindir: bindir,
				Logger: logger,
				DryRun: dryRunFlag,
			}); err != nil {
				return fmt.Errorf("manual upgrade: %w", err)
			}

			if dryRunFlag {
				break
			}

			if home, homeErr := os.UserHomeDir(); homeErr == nil && updater.DaemonInstalled(home) {
				updater.PrintDaemonRestartHint(logger)
			}
		case updater.InstallUnknown:
			logger.Warnf("Could not classify the install method. Reinstall manually:")
			logger.Warnf("  curl --retry 5 -sSfL 'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' | sh -s -- -b <your-bindir>")
		}

		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false,
		"Download installer.sh and print the exec command without running it")
	common.RootCmd.AddCommand(updateCmd)
}
