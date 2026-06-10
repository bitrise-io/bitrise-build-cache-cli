package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/doctor"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
)

//nolint:gochecknoglobals
var (
	fixFlag             bool
	jsonOutput          bool
	skipUpdateCheckFlag bool
)

//nolint:gochecknoglobals
var doctorCmd = &cobra.Command{
	Use:          "doctor",
	Short:        "Diagnose + optionally repair the local Bitrise Build Cache setup",
	Long:         `doctor runs every health check the CLI knows about — auth, proxy, ccache helper, keychain, log dirs, xcconfig, CLI version — and optionally repairs the safe ones with --fix. The only network call (GitHub release lookup) can be skipped with --no-update-check.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		report := doctorpkg.NewRunner().Run(cmd.Context(), doctorpkg.Options{
			ApplyFixes:      fixFlag,
			SkipUpdateCheck: skipUpdateCheckFlag,
		})

		if jsonOutput {
			return writeJSON(os.Stdout, report)
		}

		writeHuman(os.Stdout, report, fixFlag)

		if report.Overall() == doctorpkg.StateError {
			return fmt.Errorf("doctor reported errors")
		}

		return nil
	},
}

func writeJSON(w io.Writer, r doctorpkg.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encode report as JSON: %w", err)
	}

	return nil
}

func writeHuman(w io.Writer, r doctorpkg.Report, fixed bool) {
	if fixed {
		fmt.Fprintln(w, "Bitrise Build Cache - doctor (with --fix)")
	} else {
		fmt.Fprintln(w, "Bitrise Build Cache - doctor")
	}
	fmt.Fprintf(w, "CLI version: %s\n\n", r.Version)

	for _, it := range r.Items {
		fmt.Fprintf(w, "  %s %-22s %s\n", icon(it.Result.State), it.Name, it.Result.Detail)

		switch {
		case it.FixResult != nil:
			fmt.Fprintf(w, "      %s%s%s %s\n", colorGreen, "↳ fixed:", colorReset, *it.FixResult)
		case it.FixError != "":
			fmt.Fprintf(w, "      %s%s%s %s\n", colorRed, "↳ fix failed:", colorReset, it.FixError)
		case !fixed && it.Result.Fixable:
			fmt.Fprintf(w, "      %s%s%s rerun with --fix to repair\n", colorYellow, "↳", colorReset)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Overall: %s%s%s\n", overallColor(r.Overall()), r.Overall(), colorReset)
}

func icon(state doctorpkg.State) string {
	switch state {
	case doctorpkg.StateOK:
		return colorGreen + "✓" + colorReset
	case doctorpkg.StateWarn:
		return colorYellow + "!" + colorReset
	case doctorpkg.StateError:
		return colorRed + "✗" + colorReset
	default:
		return "?"
	}
}

func overallColor(state doctorpkg.State) string {
	switch state {
	case doctorpkg.StateOK:
		return colorGreen
	case doctorpkg.StateWarn:
		return colorYellow
	case doctorpkg.StateError:
		return colorRed
	default:
		return ""
	}
}

func init() {
	doctorCmd.Flags().BoolVar(&fixFlag, "fix", false, "Apply safe repairs in addition to diagnosing")
	doctorCmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit report as JSON instead of human-readable text")
	doctorCmd.Flags().BoolVar(&skipUpdateCheckFlag, "no-update-check", false, "Skip the GitHub release lookup (the only network call doctor makes)")
	common.RootCmd.AddCommand(doctorCmd)
}
