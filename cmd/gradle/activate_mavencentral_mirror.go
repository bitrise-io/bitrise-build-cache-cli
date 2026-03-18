package gradle

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
)

const (
	mavenCentralMirrorEnvKey = "BITRISE_MAVENCENTRAL_PROXY_ENABLED"
	mavenCentralInitFileName = "bitrise-mavencentral-mirror.init.gradle.kts"
)

//go:embed asset/mavencentral-mirror.init.gradle.kts
var mavenCentralInitScript string

var activateMavenCentralMirrorCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "mavencentral-mirror",
	Short: "Activate Bitrise MavenCentral mirror for Gradle",
	Long: `Activate Bitrise MavenCentral mirror for Gradle.
This command will install a Gradle init script that redirects MavenCentral
repository requests to a Bitrise-hosted mirror for faster dependency resolution.

The command checks the BITRISE_MAVENCENTRAL_PROXY_ENABLED environment variable
and only installs the init script when it is set to "true".`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(common.IsDebugLogMode)

		gradleHome, err := pathutil.NewPathModifier().AbsPath(gradleHomeNonExpanded)
		if err != nil {
			return fmt.Errorf("expand Gradle home path (%s), error: %w", gradleHome, err)
		}

		return ActivateMavenCentralMirrorFn(logger, gradleHome, os.Getenv)
	},
}

func init() {
	common.ActivateCmd.AddCommand(activateMavenCentralMirrorCmd)
}

// ActivateMavenCentralMirrorFn contains the main logic for the mavencentral-mirror command.
func ActivateMavenCentralMirrorFn(
	logger log.Logger,
	gradleHomePath string,
	getenv func(string) string,
) error {
	logger.TInfof("Activate Bitrise MavenCentral mirror")

	enabled := getenv(mavenCentralMirrorEnvKey)
	if enabled != "true" {
		logger.Infof("(i) %s is not set to \"true\", skipping MavenCentral mirror activation", mavenCentralMirrorEnvKey)

		return nil
	}

	initDPath := filepath.Join(gradleHomePath, "init.d")
	if err := os.MkdirAll(initDPath, 0o755); err != nil { //nolint:mnd
		return fmt.Errorf("ensure ~/.gradle/init.d exists: %w", err)
	}

	initFilePath := filepath.Join(initDPath, mavenCentralInitFileName)
	logger.Infof("(i) Writing MavenCentral mirror init script to %s", initFilePath)

	if err := os.WriteFile(initFilePath, []byte(mavenCentralInitScript), 0o644); err != nil { //nolint:gosec,mnd
		return fmt.Errorf("write %s: %w", initFilePath, err)
	}

	logger.TInfof("✅ MavenCentral mirror activated")

	return nil
}
