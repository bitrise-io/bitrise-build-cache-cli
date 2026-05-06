package common

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/update"
)

//nolint:gochecknoglobals
var (
	updateApplyVersion    string
	updateApplyJSONOutput bool
	updateCheckJSONOutput bool
)

//nolint:gochecknoglobals
var updateCmd = &cobra.Command{
	Use:           "update",
	Short:         "Check for and apply CLI updates",
	SilenceUsage:  true,
	SilenceErrors: true,
}

//nolint:gochecknoglobals
var updateCheckCmd = &cobra.Command{
	Use:           "check",
	Short:         "Check whether a newer CLI release is available on GitHub",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		current := configcommon.GetCLIVersion(nil)

		release, err := update.FetchLatestRelease(cmd.Context())
		if err != nil {
			wrappedErr := fmt.Errorf("fetch latest release: %w", err)
			if updateCheckJSONOutput {
				_ = WriteJSON(cmd.OutOrStdout(), map[string]any{"error": wrappedErr.Error()})
			}

			return wrappedErr
		}

		latest := strings.TrimPrefix(release.TagName, "v")
		newer := update.IsNewer(release.TagName, current)

		if updateCheckJSONOutput {
			return WriteJSON(cmd.OutOrStdout(), map[string]any{
				"currentVersion":  current,
				"latestVersion":   latest,
				"updateAvailable": newer,
			})
		}

		if newer {
			fmt.Fprintf(cmd.OutOrStdout(), "Update available: v%s → v%s\nRun `bitrise-build-cache update apply` to install.\n", current, latest)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Already up to date (v%s).\n", current)
		}

		return nil
	},
}

//nolint:gochecknoglobals
var updateApplyCmd = &cobra.Command{
	Use:           "apply",
	Short:         "Download and install the latest (or a specific) CLI release",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		version := strings.TrimPrefix(updateApplyVersion, "v")

		if version == "" {
			// Check first so we can skip if already current.
			current := configcommon.GetCLIVersion(nil)

			release, err := update.FetchLatestRelease(cmd.Context())
			if err != nil {
				wrappedErr := fmt.Errorf("fetch latest release: %w", err)
				if updateApplyJSONOutput {
					_ = WriteJSON(cmd.OutOrStdout(), map[string]any{"success": false, "error": wrappedErr.Error()})
				}

				return wrappedErr
			}

			version = strings.TrimPrefix(release.TagName, "v")

			if !update.IsNewer(release.TagName, current) {
				if updateApplyJSONOutput {
					return WriteJSON(cmd.OutOrStdout(), map[string]any{
						"success": true,
						"version": version,
						"message": "already up to date",
					})
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Already up to date (v%s).\n", current)

				return nil
			}
		}

		if err := update.Apply(cmd.Context(), version); err != nil {
			wrappedErr := fmt.Errorf("apply update: %w", err)
			if updateApplyJSONOutput {
				_ = WriteJSON(cmd.OutOrStdout(), map[string]any{"success": false, "error": wrappedErr.Error()})
			}

			return wrappedErr
		}

		if updateApplyJSONOutput {
			return WriteJSON(cmd.OutOrStdout(), map[string]any{
				"success": true,
				"version": version,
			})
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Updated to v%s.\n", version)

		return nil
	},
}

func init() {
	RootCmd.AddCommand(updateCmd)
	updateCmd.AddCommand(updateCheckCmd)
	updateCmd.AddCommand(updateApplyCmd)

	updateCheckCmd.Flags().BoolVar(&updateCheckJSONOutput, "json", false, "Emit machine-readable JSON to stdout")
	updateApplyCmd.Flags().StringVar(&updateApplyVersion, "version", "", "Specific version to install, e.g. v0.17.0 (default: latest)")
	updateApplyCmd.Flags().BoolVar(&updateApplyJSONOutput, "json", false, "Emit machine-readable JSON to stdout")
}
