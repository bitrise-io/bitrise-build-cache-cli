package cmd

import (
	"fmt"

	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

// addGradleVerificationReferenceDeps represents the gradle command
var addGradleVerificationReferenceDeps = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "add-reference-deps",
	Short: "Add Bitrise Build Cache plugins to the project (but do not enable them)",
	Long: `Add Bitrise Build Cache plugins to the project (but do not enable them)
This command will:

- Create a ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts file with the necessary configs. This file will be overwritten.
`,
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		//
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Add Bitrise Build Cache for Gradle plugins")
		//
		gradleHome, err := pathutil.NewPathModifier().AbsPath(gradleHomeNonExpanded)
		if err != nil {
			return fmt.Errorf("expand Gradle home path (%s), error: %w", gradleHome, err)
		}

		//
		allEnvs := utils.AllEnvs()
		if err := addGradlePluginsFn(logger, gradleHome, allEnvs); err != nil {
			return fmt.Errorf("enable Gradle Build Cache: %w", err)
		}

		logger.TInfof("✅ Bitrise Build Cache plugins added")

		return nil
	},
}

func init() {
	gradleVerification.AddCommand(addGradleVerificationReferenceDeps)
}

func addGradlePluginsFn(logger log.Logger, gradleHomePath string, envProvider map[string]string) error {
	activateGradleParams.Cache.Enabled = false
	activateGradleParams.Cache.JustDependency = true
	activateGradleParams.Analytics.Enabled = false
	activateGradleParams.Analytics.JustDependency = true
	activateGradleParams.TestDistro.Enabled = false
	activateGradleParams.TestDistro.JustDependency = true

	templateInventory, err := activateGradleParams.TemplateInventory(logger, envProvider, isDebugLogMode)
	if err != nil {
		return fmt.Errorf(FmtErrorGradleVerification, err)
	}

	if err := templateInventory.WriteToGradleInit(
		logger,
		gradleHomePath,
		utils.DefaultOsProxy{},
		gradleconfig.GradleTemplateProxy(),
	); err != nil {
		return fmt.Errorf(FmtErrorGradleVerification, err)
	}

	return nil
}

//nolint:gochecknoglobals
var (
	FmtErrorGradleVerification = "adding Gradle plugins failed: %w"
)
