package xcode

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

//nolint:gochecknoglobals
var (
	xceleratePrintStatsJSONOutput bool

	xceleratePrintStatsCmd = &cobra.Command{
		Use:           "print-stats",
		Short:         "Print current xcelerate proxy session statistics",
		Long:          "Prints the current session statistics of the running xcelerate proxy without sending analytics.\n\nExample:\n  bitrise-build-cache xcelerate print-stats\n  bitrise-build-cache xcelerate print-stats --json",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			osProxy := utils.DefaultOsProxy{}

			config, err := xcelerate.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
			if err != nil {
				wrappedErr := fmt.Errorf("read xcelerate config: %w", err)
				if xceleratePrintStatsJSONOutput {
					_ = common.WriteJSON(cmd.OutOrStdout(), map[string]any{"error": wrappedErr.Error()})
				}

				return wrappedErr
			}

			logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
			client, cleanup := createProxySessionClient(config, logger)
			defer cleanup()

			if client == nil {
				err := fmt.Errorf("xcelerate proxy is not running or unreachable at %s", config.ProxySocketPath)
				if xceleratePrintStatsJSONOutput {
					_ = common.WriteJSON(cmd.OutOrStdout(), map[string]any{"error": err.Error()})
				}

				return err
			}

			proxyStats, err := client.GetSessionStats(cmd.Context(), &empty.Empty{})
			if err != nil {
				wrappedErr := fmt.Errorf("get session stats: %w", err)
				if xceleratePrintStatsJSONOutput {
					_ = common.WriteJSON(cmd.OutOrStdout(), map[string]any{"error": wrappedErr.Error()})
				}

				return wrappedErr
			}

			type statsOutput struct {
				DownloadedBytes int64   `json:"downloadedBytes"`
				UploadedBytes   int64   `json:"uploadedBytes"`
				Uploads         int64   `json:"uploads"`
				Hits            int64   `json:"hits"`
				Misses          int64   `json:"misses"`
				HitRate         float64 `json:"hitRate"`
				KvHits          int64   `json:"kvHits"`
				KvMisses        int64   `json:"kvMisses"`
				KvHitRate       float64 `json:"kvHitRate"`
				KvUploadedBytes int64   `json:"kvUploadedBytes"`
			}

			hits := proxyStats.GetHits()
			misses := proxyStats.GetMisses()
			kvHits := proxyStats.GetKvHits()
			kvMisses := proxyStats.GetKvMisses()

			out := statsOutput{
				DownloadedBytes: proxyStats.GetDownloadedBytes(),
				UploadedBytes:   proxyStats.GetUploadedBytes(),
				Uploads:         proxyStats.GetUploads(),
				Hits:            hits,
				Misses:          misses,
				KvHits:          kvHits,
				KvMisses:        kvMisses,
				KvUploadedBytes: proxyStats.GetKvUploadedBytes(),
			}

			if hits+misses > 0 {
				out.HitRate = float64(hits) / float64(hits+misses)
			}

			if kvHits+kvMisses > 0 {
				out.KvHitRate = float64(kvHits) / float64(kvHits+kvMisses)
			}

			if xceleratePrintStatsJSONOutput {
				return common.WriteJSON(cmd.OutOrStdout(), out)
			}

			printXcelerateStats(cmd, out.DownloadedBytes, out.UploadedBytes, out.Uploads,
				hits, misses, out.HitRate, kvHits, kvMisses, out.KvHitRate, out.KvUploadedBytes)

			return nil
		},
	}
)

func init() {
	xceleratePrintStatsCmd.Flags().BoolVar(&xceleratePrintStatsJSONOutput, "json", false, "Emit machine-readable JSON to stdout")
	xcelerateCommand.AddCommand(xceleratePrintStatsCmd)
}

func printXcelerateStats(
	cmd *cobra.Command,
	downloadedBytes, uploadedBytes, uploads int64,
	hits, misses int64, hitRate float64,
	kvHits, kvMisses int64, kvHitRate float64,
	kvUploadedBytes int64,
) {
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Xcelerate proxy session stats\n\n")

	fmt.Fprintf(out, "Transfer\n")
	fmt.Fprintf(out, "  Downloaded: %s\n", formatXcelerateBytes(downloadedBytes))
	fmt.Fprintf(out, "  Uploaded:   %s\n", formatXcelerateBytes(uploadedBytes))
	fmt.Fprintf(out, "  Uploads:    %d\n", uploads)

	total := hits + misses
	if total > 0 {
		fmt.Fprintf(out, "\nBlob cache\n")
		fmt.Fprintf(out, "  Hits:   %d\n", hits)
		fmt.Fprintf(out, "  Misses: %d\n", misses)
		fmt.Fprintf(out, "  Total:  %d (%.1f%% hit rate)\n", total, hitRate*100)
	}

	kvTotal := kvHits + kvMisses
	if kvTotal > 0 {
		fmt.Fprintf(out, "\nKV cache\n")
		fmt.Fprintf(out, "  Hits:     %d\n", kvHits)
		fmt.Fprintf(out, "  Misses:   %d\n", kvMisses)
		fmt.Fprintf(out, "  Total:    %d (%.1f%% hit rate)\n", kvTotal, kvHitRate*100)
		fmt.Fprintf(out, "  Uploaded: %s\n", formatXcelerateBytes(kvUploadedBytes))
	}
}

func formatXcelerateBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)

	switch {
	case b >= gb:
		return fmt.Sprintf("%.2f GiB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MiB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KiB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
