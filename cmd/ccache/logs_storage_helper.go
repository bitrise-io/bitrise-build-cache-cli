package ccache

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/logtail"
)

//nolint:gochecknoglobals
var (
	logsLines int
	logsTail  bool
	logsJSON  bool
)

//nolint:gochecknoglobals
var logsStorageHelperCmd = &cobra.Command{
	Use:           "logs",
	Short:         "Show or tail ccache storage-helper logs",
	Long:          "Prints the last --lines lines from the storage-helper out and err logs. With --tail, streams new content until interrupted.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}

		logDir := filepath.Join(home, ".local", "state", "ccache", "logs")

		outPath, err := logtail.MostRecentGlob(filepath.Join(logDir, "ccache-*.log"))
		if err != nil {
			return fmt.Errorf("find ccache log: %w", err)
		}
		if outPath == "" && !logsTail {
			return fmt.Errorf("no ccache log files found in %s", logDir)
		}

		sources := []logtail.Source{
			{Label: "out", Path: outPath},
			{Label: "err", Path: filepath.Join(logDir, "ccache-err.log")},
		}

		return logtail.Tail(cmd.Context(), cmd.OutOrStdout(), sources, logtail.Opts{
			Lines:  logsLines,
			Follow: logsTail,
			JSON:   logsJSON,
		})
	},
}

func init() {
	logsStorageHelperCmd.Flags().IntVarP(&logsLines, "lines", "n", 50, "Number of trailing lines to show")
	logsStorageHelperCmd.Flags().BoolVarP(&logsTail, "tail", "f", false, "Follow log output")
	logsStorageHelperCmd.Flags().BoolVar(&logsJSON, "json", false, "Emit JSON lines for app pipe-through")

	storageHelperCmd.AddCommand(logsStorageHelperCmd)
}
