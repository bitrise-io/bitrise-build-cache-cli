package xcode

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/logtail"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

//nolint:gochecknoglobals
var (
	xcelerateLogsLines int
	xcelerateLogsTail  bool
	xcelerateLogsJSON  bool
)

//nolint:gochecknoglobals
var xcelerateLogsCmd = &cobra.Command{
	Use:           "logs",
	Short:         "Show or tail xcelerate proxy logs",
	Long:          "Prints the last --lines lines from the proxy out and err logs. With --tail, streams new content until interrupted.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		osProxy := utils.DefaultOsProxy{}

		logDir, err := getLogDir(osProxy)
		if err != nil {
			return fmt.Errorf("get log dir: %w", err)
		}

		outPath, err := logtail.MostRecentGlob(filepath.Join(logDir, "proxy-*-out.log"))
		if err != nil {
			return fmt.Errorf("find proxy log: %w", err)
		}
		if outPath == "" && !xcelerateLogsTail {
			return fmt.Errorf("no xcelerate proxy log files found in %s", logDir)
		}

		sources := []logtail.Source{
			{Label: "out", Path: outPath},
			{Label: "err", Path: filepath.Join(logDir, proxyErr)},
		}

		return logtail.Tail(cmd.Context(), cmd.OutOrStdout(), sources, logtail.Opts{
			Lines:  xcelerateLogsLines,
			Follow: xcelerateLogsTail,
			JSON:   xcelerateLogsJSON,
		})
	},
}

func init() {
	xcelerateLogsCmd.Flags().IntVarP(&xcelerateLogsLines, "lines", "n", 50, "Number of trailing lines to show")
	xcelerateLogsCmd.Flags().BoolVarP(&xcelerateLogsTail, "tail", "f", false, "Follow log output")
	xcelerateLogsCmd.Flags().BoolVar(&xcelerateLogsJSON, "json", false, "Emit JSON lines for app pipe-through")

	xcelerateCommand.AddCommand(xcelerateLogsCmd)
}
