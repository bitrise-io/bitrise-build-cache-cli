package cmd

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/diagnostics"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

var saveGradleOutputDataCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "save-gradle-output-data",
	Short: "Save Gradle output data to cache, for running diagnostics builds",
	Long: `Save Gradle output data to cache, for running diagnostics builds.

	This command will:
- Collect the contents of **/build/ + .gradle/ directories.
- Save the collected data to the Bitrise key-value cache.
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Save Gradle output data to cache, for running diagnostics builds")

		additionalPaths, err := cmd.Flags().GetStringSlice("additional-paths")
		if err != nil {
			return fmt.Errorf("get additional paths flag: %w", err)
		}

		return saveGradleOutputDataCmdFn(logger, additionalPaths)
	},
}

func init() {
	saveGradleOutputDataCmd.Flags().StringSlice("additional-paths",
		[]string{},
		"Additional paths to save, relative to the current working directory. These paths will be added to the default paths: **/build/ + .gradle/")
	rootCmd.AddCommand(saveGradleOutputDataCmd)
}

func saveGradleOutputDataCmdFn(logger log.Logger, additionalPaths []string) error {
	pathChecker := pathutil.NewPathChecker()
	pathProvider := pathutil.NewPathProvider()
	pathModifier := pathutil.NewPathModifier()
	envRepo := env.NewRepository()

	saveGradleDiagnosticOutputStep := diagnostics.NewGradleDiagnosticOuptutSaver(logger, pathChecker, pathProvider, pathModifier, envRepo)

	if err := saveGradleDiagnosticOutputStep.Run(isDebugLogMode, additionalPaths); err != nil {
		return fmt.Errorf("save Gradle output: %w", err)
	}

	logger.TInfof("✅ Gradle output data saved to cache")

	return nil
}
