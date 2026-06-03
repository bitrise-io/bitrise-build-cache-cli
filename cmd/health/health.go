package health

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	healthpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/health"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
)

//nolint:gochecknoglobals
var jsonOutput bool

//nolint:gochecknoglobals
var healthCmd = &cobra.Command{
	Use:          "health",
	Short:        "Show the health of the local Bitrise Build Cache setup",
	Long:         `Show the health of the local Bitrise Build Cache setup — proxy state, auth credentials, CLI version. Default output is human-readable. Use --json for tooling.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		runner := healthpkg.NewRunner()
		s := runner.Run(cmd.Context())

		if jsonOutput {
			return writeJSON(os.Stdout, s)
		}

		writeHuman(os.Stdout, s)

		if s.Overall() == healthpkg.StateError {
			return fmt.Errorf("status reported errors")
		}

		return nil
	},
}

func writeJSON(w *os.File, s healthpkg.Status) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return fmt.Errorf("encode status as JSON: %w", err)
	}

	return nil
}

func writeHuman(w *os.File, s healthpkg.Status) {
	fmt.Fprintf(w, "Bitrise Build Cache CLI %s\n\n", s.Version)
	for _, c := range s.Checks {
		fmt.Fprintf(w, "  %s %-18s %s\n", icon(c.State), c.Name, c.Detail)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Overall: %s%s%s\n", overallColor(s.Overall()), s.Overall(), colorReset)
}

func icon(state healthpkg.State) string {
	switch state {
	case healthpkg.StateOK:
		return colorGreen + "✓" + colorReset
	case healthpkg.StateWarn:
		return colorYellow + "!" + colorReset
	case healthpkg.StateError:
		return colorRed + "✗" + colorReset
	default:
		return "?"
	}
}

func overallColor(state healthpkg.State) string {
	switch state {
	case healthpkg.StateOK:
		return colorGreen
	case healthpkg.StateWarn:
		return colorYellow
	case healthpkg.StateError:
		return colorRed
	default:
		return ""
	}
}

func init() {
	healthCmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit status as JSON instead of human-readable text")
	common.RootCmd.AddCommand(healthCmd)
}
