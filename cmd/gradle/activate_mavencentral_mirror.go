package gradle

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

const (
	mavenCentralMirrorEnvKey = "BITRISE_MAVENCENTRAL_PROXY_ENABLED"
	datacenterEnvKey         = "BITRISE_DEN_VM_DATACENTER"
	mavenCentralInitFileName = "bitrise-mavencentral-mirror.init.gradle.kts"
	mirrorURLTemplate        = "https://repository-manager-%s.services.bitrise.io:8090/maven/central"
)

//go:embed asset/mavencentral-mirror.init.gradle.kts.gotemplate
var mavenCentralInitTemplate string

type mavenCentralMirrorTemplateData struct {
	MirrorURL string
}

var activateMavenCentralMirrorCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "mavencentral-mirror",
	Short: "Activate Bitrise MavenCentral mirror for Gradle",
	Long: `Activate Bitrise MavenCentral mirror for Gradle.
This command will install a Gradle init script that redirects MavenCentral
repository requests to a Bitrise-hosted mirror for faster dependency resolution.

The command checks the BITRISE_MAVENCENTRAL_PROXY_ENABLED environment variable
and only installs the init script when it is set to "true".
The mirror URL is determined by the BITRISE_DEN_VM_DATACENTER environment variable.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(common.IsDebugLogMode)

		gradleHome, err := pathutil.NewPathModifier().AbsPath(gradleHomeNonExpanded)
		if err != nil {
			return fmt.Errorf("expand Gradle home path (%s), error: %w", gradleHome, err)
		}

		allEnvs := utils.AllEnvs()

		return ActivateMavenCentralMirrorFn(logger, gradleHome, allEnvs)
	},
}

func init() {
	common.ActivateCmd.AddCommand(activateMavenCentralMirrorCmd)
}

// datacenterToMirrorRegion converts a datacenter env value (e.g. "AMS1", "IAD1", "ORD1")
// to the mirror region slug by lowercasing and stripping trailing digits.
func datacenterToMirrorRegion(dc string) string {
	lower := strings.ToLower(dc)

	return strings.TrimRightFunc(lower, unicode.IsDigit)
}

// ActivateMavenCentralMirrorFn contains the main logic for the mavencentral-mirror command.
func ActivateMavenCentralMirrorFn(
	logger log.Logger,
	gradleHomePath string,
	envProvider map[string]string,
) error {
	enabled := envProvider[mavenCentralMirrorEnvKey]
	if enabled != "true" {
		fmt.Fprintf(os.Stdout, "%s is not set to \"true\", skipping MavenCentral mirror activation\n", mavenCentralMirrorEnvKey)

		return nil
	}

	dc := envProvider[datacenterEnvKey]
	if dc == "" {
		fmt.Fprintf(os.Stdout, "%s is not set, skipping MavenCentral mirror activation (e.g. local dev environment)\n", datacenterEnvKey)

		return nil
	}

	region := datacenterToMirrorRegion(dc)
	mirrorURL := fmt.Sprintf(mirrorURLTemplate, region)
	logger.Debugf("Datacenter: %s, mirror region: %s, mirror URL: %s", dc, region, mirrorURL)

	tmpl, err := template.New("mavencentral-mirror").Parse(mavenCentralInitTemplate)
	if err != nil {
		return fmt.Errorf("parse mavencentral-mirror init template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, mavenCentralMirrorTemplateData{MirrorURL: mirrorURL}); err != nil {
		return fmt.Errorf("execute mavencentral-mirror init template: %w", err)
	}

	initDPath := filepath.Join(gradleHomePath, "init.d")
	if err := os.MkdirAll(initDPath, 0o755); err != nil { //nolint:mnd
		return fmt.Errorf("ensure ~/.gradle/init.d exists: %w", err)
	}

	initFilePath := filepath.Join(initDPath, mavenCentralInitFileName)
	logger.Debugf("Writing MavenCentral mirror init script to %s", initFilePath)

	if err := os.WriteFile(initFilePath, buf.Bytes(), 0o644); err != nil { //nolint:gosec,mnd
		return fmt.Errorf("write %s: %w", initFilePath, err)
	}

	fmt.Fprintln(os.Stdout, "MavenCentral mirror activated")

	return nil
}
