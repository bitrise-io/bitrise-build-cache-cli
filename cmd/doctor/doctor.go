package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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
	Long:         `doctor runs every health check the CLI knows about — auth, proxy, ccache helper, keychain, log dirs, CLI version — and optionally repairs the safe ones with --fix. The only network call (GitHub release lookup) can be skipped with --no-update-check.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()

		report := doctorpkg.NewRunner().Run(cmd.Context(), doctorpkg.Options{
			ApplyFixes:      fixFlag,
			SkipUpdateCheck: skipUpdateCheckFlag,
		})

		if jsonOutput {
			return writeJSON(out, report)
		}

		writeHuman(out, report, fixFlag, colorEnabled(out))

		if report.Overall() == doctorpkg.StateError {
			return errors.New("doctor reported errors")
		}

		return nil
	},
}

// colorEnabled honours NO_COLOR (https://no-color.org) and falls back to TTY detection.
func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}

	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}

	return false
}

func writeJSON(w io.Writer, r doctorpkg.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encode report as JSON: %w", err)
	}

	return nil
}

func writeHuman(w io.Writer, r doctorpkg.Report, fixed bool, colored bool) {
	c := palette(colored)

	if fixed {
		fmt.Fprintln(w, "Bitrise Build Cache - doctor (with --fix)")
	} else {
		fmt.Fprintln(w, "Bitrise Build Cache - doctor")
	}
	fmt.Fprintf(w, "CLI version: %s\n\n", r.Version)

	for _, it := range r.Items {
		fmt.Fprintf(w, "  %s %-22s %s\n", c.icon(it.Result.State), it.Name, it.Result.Detail)

		switch {
		case it.FixResult != nil:
			fmt.Fprintf(w, "      %s↳ fixed:%s %s\n", c.green, c.reset, *it.FixResult)
		case it.FixError != "":
			fmt.Fprintf(w, "      %s↳ fix failed:%s %s\n", c.red, c.reset, it.FixError)
		case !fixed && it.Result.Fixable:
			fmt.Fprintf(w, "      %s↳%s rerun with --fix to repair\n", c.yellow, c.reset)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Overall: %s%s%s\n", c.forState(r.Overall()), r.Overall(), c.reset)
}

type colorPalette struct {
	reset, green, yellow, red string
}

func palette(enabled bool) colorPalette {
	if !enabled {
		return colorPalette{}
	}

	return colorPalette{
		reset:  colorReset,
		green:  colorGreen,
		yellow: colorYellow,
		red:    colorRed,
	}
}

func (c colorPalette) icon(state doctorpkg.State) string {
	switch state {
	case doctorpkg.StateOK:
		return c.green + "✓" + c.reset
	case doctorpkg.StateWarn:
		return c.yellow + "!" + c.reset
	case doctorpkg.StateError:
		return c.red + "✗" + c.reset
	default:
		return "?"
	}
}

func (c colorPalette) forState(state doctorpkg.State) string {
	switch state {
	case doctorpkg.StateOK:
		return c.green
	case doctorpkg.StateWarn:
		return c.yellow
	case doctorpkg.StateError:
		return c.red
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
