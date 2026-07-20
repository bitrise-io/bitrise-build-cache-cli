// Package invocations exposes the `invocations` cobra subcommand for browsing the local NDJSON invocation log.
package invocations

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	invpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/invocations"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

const (
	sourceLocal = "local"
	sourceCI    = "ci"
	sourceAll   = "all"
)

//nolint:gochecknoglobals
var invocationsCmd = &cobra.Command{
	Use:          "invocations",
	Short:        "Browse local build-cache invocation history",
	SilenceUsage: true,
}

//nolint:gochecknoglobals
var listFlags struct {
	limit  int
	json   bool
	source string
}

//nolint:gochecknoglobals
var listCmd = &cobra.Command{
	Use:          "list",
	Short:        "List recent invocations from the local NDJSON log",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		match, err := matcherFor(listFlags.source)
		if err != nil {
			return err
		}

		p, err := paths.Default()
		if err != nil {
			return fmt.Errorf("resolve paths: %w", err)
		}

		records, err := invpkg.NewReader(p).RecentMatching(listFlags.limit, match)
		if err != nil {
			return fmt.Errorf("read invocations: %w", err)
		}

		if listFlags.json {
			return writeJSON(cmd.OutOrStdout(), records)
		}

		return writeTable(cmd.OutOrStdout(), records)
	},
}

func matcherFor(source string) (func(invpkg.Record) bool, error) {
	switch source {
	case sourceAll:
		return nil, nil
	case sourceLocal:
		return func(r invpkg.Record) bool { return r.IsLocal() }, nil
	case sourceCI:
		return func(r invpkg.Record) bool { return !r.IsLocal() }, nil
	default:
		return nil, fmt.Errorf("--source must be one of %s|%s|%s (got %q)", sourceLocal, sourceCI, sourceAll, source)
	}
}

func writeJSON(out io.Writer, records []invpkg.Record) error {
	if records == nil {
		records = []invpkg.Record{}
	}

	enc := json.NewEncoder(out)
	if err := enc.Encode(records); err != nil {
		return fmt.Errorf("encode invocations JSON: %w", err)
	}

	return nil
}

func writeTable(out io.Writer, records []invpkg.Record) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	if _, err := fmt.Fprintln(tw, "ID\tTOOL\tCOMMAND\tDURATION\tHIT RATE\tSTATUS\tTIMESTAMP"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	for _, rec := range records {
		duration := "-"
		if !rec.FinishedAt.IsZero() {
			duration = rec.FinishedAt.Sub(rec.StartedAt).Round(time.Second).String()
		}

		// hit_rate is omitted when zero — cannot distinguish unset from a real 0%
		hitRate := "-"
		if rec.HitRate > 0 {
			hitRate = fmt.Sprintf("%.0f%%", rec.HitRate*100)
		}

		status := "success"
		if rec.ExitCode != 0 {
			status = "failed"
		}

		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			rec.InvocationID,
			rec.Tool,
			rec.Command,
			duration,
			hitRate,
			status,
			rec.StartedAt.Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}

	return nil
}

//nolint:gochecknoinits
func init() {
	listCmd.Flags().IntVar(&listFlags.limit, "limit", 10, "Maximum number of records to return.")
	listCmd.Flags().BoolVar(&listFlags.json, "json", false, "Emit records as JSON instead of a text table.")
	listCmd.Flags().StringVar(&listFlags.source, "source", sourceLocal, "Filter by origin: local|ci|all.")

	invocationsCmd.AddCommand(listCmd)
	common.RootCmd.AddCommand(invocationsCmd)
}
