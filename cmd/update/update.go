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

		return updater.Update(cmd.Context(), updater.Options{
			Executable: exe,
			Logger:     logger,
			DryRun:     dryRunFlag,
		})
	},
}

func init() {
	updateCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false,
		"Download installer.sh and print the exec command without running it")
	common.RootCmd.AddCommand(updateCmd)
}
