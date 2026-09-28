package bazel

import (
	"fmt"
	"path"

	"github.com/spf13/cobra"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

//nolint:gochecknoglobals
var printCredHelperLineDir string

//nolint:gochecknoglobals
var printCredHelperLineCmd = &cobra.Command{
	Use:   "print-credhelper-line",
	Short: "Print the .bazelrc snippet that points --credential_helper at the shim",
	Long: `Print the .bazelrc snippet that points --credential_helper at the shim
` + paths.BazelCredHelperShimName + ` produced by ` + "`" + `install-credhelper-shim` + "`" + `.
Redirect the output into your repo-level .bazelrc.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir := printCredHelperLineDir
		if dir == "" {
			dir = paths.BazelCredHelperShimDefaultDir
		}
		rel := path.Join(dir, paths.BazelCredHelperShimName)

		_, err := fmt.Fprint(cmd.OutOrStdout(), bazelconfig.CredHelperLineSnippet(rel))
		if err != nil {
			return fmt.Errorf("write snippet: %w", err)
		}

		return nil
	},
}

func init() {
	printCredHelperLineCmd.Flags().StringVar(&printCredHelperLineDir, "dir", paths.BazelCredHelperShimDefaultDir,
		"Workspace-relative dir the shim lives in (matches install-credhelper-shim --dir).")
	bazelCmd.AddCommand(printCredHelperLineCmd)
}
