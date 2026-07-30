package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
)

//nolint:gochecknoglobals
var (
	fixFlag              bool
	interactiveFlag      bool
	jsonOutput           bool
	skipUpdateCheckFlag  bool
	skipBackendProbeFlag bool
)

//nolint:gochecknoglobals
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose + optionally repair the local Bitrise Build Cache setup",
	Long: `doctor runs every health check the CLI knows about — auth, proxy, ccache helper,
keychain, log dirs, CLI version — and can repair some of them.

  doctor                      diagnose only, change nothing.
  doctor --fix                repair everything repairable without asking, like
                              ` + "`lint --fix`" + `. Repairs that need a prompt (setting
                              credentials) are skipped and reported as such.
  doctor --fix --interactive  show the results, then choose what to repair, with
                              errors preselected. This is the one that can set up
                              credentials, offering a browser sign-in first.

Network calls (GitHub release lookup, Build Cache backend probe) can be skipped
with --no-update-check / --no-backend-probe.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()

		// --interactive is about choosing repairs, so it implies --fix.
		if interactiveFlag {
			fixFlag = true

			if jsonOutput {
				return errors.New("--interactive cannot be combined with --json: there is nowhere to ask")
			}
			if !interactiveStdout(out) {
				return errors.New("--interactive needs a terminal. Use --fix on its own to repair everything unprompted")
			}
		}

		d := doctorpkg.NewDoctor()
		d.Debug = common.IsDebugLogMode
		d.AuthFixPrompt = common.FixAuthPrompt(cmd.Context(), log.NewLogger(log.WithDebugLog(common.IsDebugLogMode)))

		opts := doctorpkg.Options{
			SkipUpdateCheck:  skipUpdateCheckFlag,
			SkipBackendProbe: skipBackendProbeFlag,
		}

		// Diagnose first so the results can be shown before anything is changed,
		// and so the user can choose what to repair.
		report := d.Diagnose(cmd.Context(), opts)

		if jsonOutput {
			if fixFlag {
				doctorpkg.ApplyFixesUnattended(&report)
			}
			if err := writeJSON(out, report); err != nil {
				return err
			}

			return doctorExit(report)
		}

		switch {
		case interactiveFlag:
			// The recap comes first so the choice is an informed one.
			writeHuman(out, report, false, doctorpkg.EffectiveOverall(report), colorEnabled(out))

			if err := applyChosenFixes(out, &report, colorEnabled(out)); err != nil {
				return err
			}
		case fixFlag:
			// Unprompted, like `lint --fix`: repair what can be repaired silently.
			doctorpkg.ApplyFixesUnattended(&report)
		default:
			writeHuman(out, report, false, doctorpkg.EffectiveOverall(report), colorEnabled(out))

			return doctorExit(report)
		}

		writeHuman(out, report, true, doctorpkg.EffectiveOverall(report), colorEnabled(out))

		return doctorExit(report)
	},
}

func doctorExit(r doctorpkg.Report) error {
	if doctorpkg.EffectiveOverall(r) == doctorpkg.StateError {
		return errors.New("doctor reported errors")
	}

	return nil
}

// applyChosenFixes asks which of the fixable issues to repair, with errors
// preselected, and applies only those.
func applyChosenFixes(out io.Writer, report *doctorpkg.Report, colored bool) error {
	fixable := doctorpkg.Fixable(report.Items)
	if len(fixable) == 0 {
		fmt.Fprintln(out, "Nothing here can be repaired automatically.")

		return nil
	}

	chosen, err := pickFixes(fixable, colored)
	if err != nil {
		if errors.Is(err, tui.ErrAborted) {
			fmt.Fprintln(out, "Nothing was changed.")

			return nil
		}

		return err
	}
	if len(chosen) == 0 {
		fmt.Fprintln(out, "Nothing selected, so nothing was changed.")

		return nil
	}

	for i := range report.Items {
		if chosen[report.Items[i].Name] {
			doctorpkg.ApplyFix(&report.Items[i])
		}
	}

	return nil
}

// pickFixes returns the names the user chose. Errors start selected because they
// are what actually breaks a build; warnings are offered but left unticked.
func pickFixes(fixable []doctorpkg.ReportItem, colored bool) (map[string]bool, error) {
	c := palette(colored)

	options, selected := fixOptions(fixable, c)

	description := "Errors are selected; space toggles, enter confirms."
	picked := selected
	if err := tui.RunForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Which of these should I repair?").
			Description(description).
			Options(options...).
			Height(len(options) + tui.Chrome(description)).
			Value(&picked),
	)); err != nil {
		return nil, err //nolint:wrapcheck // ErrAborted, or already wrapped
	}

	out := make(map[string]bool, len(picked))
	for _, name := range picked {
		out[name] = true
	}

	return out, nil
}

// fixOptions builds the picker's options and the initial selection. Errors start
// ticked because they are what actually breaks a build; warnings are offered but
// left for the user to opt into.
func fixOptions(fixable []doctorpkg.ReportItem, c colorPalette) ([]huh.Option[string], []string) {
	options := make([]huh.Option[string], 0, len(fixable))
	selected := make([]string, 0, len(fixable))

	for _, it := range fixable {
		state, detail := doctorpkg.ItemDisplay(it)
		options = append(options, huh.NewOption(
			fmt.Sprintf("%s %s — %s", c.icon(state), it.Name, detail), it.Name))
		if state == doctorpkg.StateError {
			selected = append(selected, it.Name)
		}
	}

	return options, selected
}

// interactiveStdout reports whether we can prompt: a redirected stdout means a
// script or a pipe is consuming the report.
func interactiveStdout(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}

	return term.IsTerminal(int(f.Fd()))
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

func writeHuman(w io.Writer, r doctorpkg.Report, fixed bool, overall doctorpkg.State, colored bool) {
	c := palette(colored)

	if fixed {
		fmt.Fprintln(w, "Bitrise Build Cache - doctor (with --fix)")
	} else {
		fmt.Fprintln(w, "Bitrise Build Cache - doctor")
	}
	fmt.Fprintf(w, "CLI version: %s\n\n", r.Version)

	issues, healthy := doctorpkg.Partition(r.Items)

	if len(issues) > 0 {
		fmt.Fprintln(w, "Issues:")
		for _, it := range issues {
			writeItem(w, c, it, fixed)
		}

		fmt.Fprintln(w)
	}

	if len(healthy) > 0 {
		fmt.Fprintln(w, "Healthy:")
		for _, it := range healthy {
			writeItem(w, c, it, fixed)
		}

		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Overall: %s%s%s\n", c.forState(overall), overall, c.reset)
}

func writeItem(w io.Writer, c colorPalette, it doctorpkg.ReportItem, fixed bool) {
	state, detail := doctorpkg.ItemDisplay(it)
	fmt.Fprintf(w, "  %s %-22s %s\n", c.icon(state), it.Name, detail)

	switch {
	case it.FixError != "":
		fmt.Fprintf(w, "      %s↳ fix failed:%s %s\n", c.red, c.reset, it.FixError)
	case !fixed && it.Result.Fixable:
		fmt.Fprintf(w, "      %s↳%s rerun with `--fix --interactive` to choose what to repair\n", c.yellow, c.reset)
	}
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
	doctorCmd.Flags().BoolVar(&fixFlag, "fix", false,
		"Repair everything that can be repaired without asking. Fixes needing a prompt (credentials) are skipped — use --interactive for those.")
	doctorCmd.Flags().BoolVar(&interactiveFlag, "interactive", false,
		"Choose which issues to repair, with errors preselected. Implies --fix and needs a terminal.")
	doctorCmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit report as JSON instead of human-readable text")
	doctorCmd.Flags().BoolVar(&skipUpdateCheckFlag, "no-update-check", false, "Skip the GitHub release lookup")
	doctorCmd.Flags().BoolVar(&skipBackendProbeFlag, "no-backend-probe", false, "Skip the Build Cache backend auth probe (sentinel KV PUT)")
	common.RootCmd.AddCommand(doctorCmd)
}
