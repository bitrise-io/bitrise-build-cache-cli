// Package project holds subcommands that inspect per-project scoping state so
// tool activators do not each re-implement the walkup + machine-config read.
package project

import (
	"fmt"
	"os"
	"path/filepath"

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
	Short:        "Inspect and manage per-project scoping state",
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

		if machineconfig.ResolvedProjectMode(current) != machineconfig.ModeOptIn {
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

//nolint:gochecknoglobals
var enableCmd = &cobra.Command{
	Use:   "enable [dir]",
	Short: "Opt this project in by writing the .bitrise-build-cache.json marker",
	Long: "Writes an empty .bitrise-build-cache.json marker at [dir] (or the current directory " +
		"if [dir] is omitted) so the CLI treats this project as opted-in when project-mode is 'opt-in'. " +
		"No-op when a marker already exists at the target or any ancestor.",
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := resolveTargetDir(args)
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "resolve target dir: %s\n", err)

			return cobraExit(ExitError)
		}

		osProxy := utils.DefaultOsProxy{}
		found, ancestor, err := machineconfig.FindMarker(dir, osProxy)
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "walk up for marker: %s\n", err)

			return cobraExit(ExitError)
		}
		if found {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[bitrise-build-cache] marker already covers %s (found at %s)\n", dir, ancestor)

			return nil
		}

		target := filepath.Join(dir, paths.ProjectMarkerFilename)
		if err := osProxy.WriteFile(target, []byte("{}\n"), 0o644); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "write marker: %s\n", err)

			return cobraExit(ExitError)
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[bitrise-build-cache] wrote marker at %s\n", target)

		return nil
	},
}

func resolveTargetDir(args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}

	return cwd, nil
}

func cobraExit(code int) error { return common.ExitCodeError{Code: code} }

func init() {
	scopeCheckCmd.Flags().BoolVar(&scopeCheckQuiet, "quiet", false, "Suppress the stderr explanation on active and gated exits.")

	projectCmd.AddCommand(scopeCheckCmd)
	projectCmd.AddCommand(enableCmd)
	common.RootCmd.AddCommand(projectCmd)
}
