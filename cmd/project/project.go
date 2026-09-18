// Package project holds subcommands that inspect per-project scoping state so
// tool activators do not each re-implement the walkup + machine-config read.
package project

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Exit codes are contract with initd.gradle.kts.gotemplate; changing them
// requires a matching template bump.
const (
	ExitActive = 0
	ExitGated  = 1
	ExitError  = 2
)

func ResetForTest() { scopeCheckQuiet = false }

//nolint:gochecknoglobals
var projectCmd = &cobra.Command{
	Use:          "project",
	Short:        "Inspect per-project scoping state",
	SilenceUsage: true,
}

//nolint:gochecknoglobals
var scopeCheckQuiet bool

//nolint:gochecknoglobals
var scopeCheckCmd = &cobra.Command{
	Use:   "scope-check <dir>",
	Short: "Report whether a directory is in scope for build-cache activation",
	Long: "Exits 0 when scope is active (mode=always, or mode=opt-in and the .bitrise-build-cache.json marker " +
		"is found in <dir> or any ancestor). Exits 1 when scope is gated (mode=opt-in, no marker). " +
		"Exits >=2 on error. Prints nothing to stdout; on the active/gated paths prints a one-line " +
		"explanation to stderr unless --quiet is set.",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		osProxy := utils.DefaultOsProxy{}

		p, err := paths.Default()
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "resolve home dir: %s\n", err)

			return cobraExit(ExitError)
		}

		current, err := machineconfig.Read(osProxy, p, nil)
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "read machine config: %s\n", err)

			return cobraExit(ExitError)
		}

		effective, _, err := machineconfig.Effective(machineconfig.FlagOverlay{}, current)
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "resolve machine config: %s\n", err)

			return cobraExit(ExitError)
		}

		if effective.ProjectMode != machineconfig.ModeOptIn {
			return nil
		}

		found, markerPath, err := machineconfig.FindMarker(args[0], osProxy)
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "walk up for marker: %s\n", err)

			return cobraExit(ExitError)
		}

		if found {
			if scopeCheckQuiet {
				return nil
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[bitrise-build-cache] project-mode=opt-in, marker at %s\n", markerPath)

			return nil
		}

		if !scopeCheckQuiet {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"[bitrise-build-cache] project-mode=opt-in, no %s found at or above %s; skipping activation\n",
				paths.ProjectMarkerFilename, args[0])
		}

		return cobraExit(ExitGated)
	},
}

func cobraExit(code int) error { return common.ExitCodeError{Code: code} }

func init() {
	scopeCheckCmd.Flags().BoolVar(&scopeCheckQuiet, "quiet", false, "Suppress the stderr explanation on active and gated exits.")

	projectCmd.AddCommand(scopeCheckCmd)
	common.RootCmd.AddCommand(projectCmd)
}
