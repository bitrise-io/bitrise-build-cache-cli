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
	gradleMirrorsEnvKey    = "BITRISE_MAVENCENTRAL_PROXY_ENABLED"
	datacenterEnvKey       = "BITRISE_DEN_VM_DATACENTER"
	gradleMirrorsInitFile  = "bitrise-gradle-mirrors.init.gradle.kts"
	gradleMirrorURLPattern = "https://repository-manager-%s.services.bitrise.io:8090/maven/%s"
)

// RepoMirror describes a single repository that can be mirrored.
type RepoMirror struct {
	FlagName                string // cobra flag name, e.g. "mavencentral"
	TemplateID              string // unique suffix for Kotlin variable names, e.g. "Central"
	URLSegment              string // last path segment in the mirror URL, e.g. "central"
	GradleMatch             string // Kotlin predicate body (using `r` as the repo) that decides whether the repo should be mirrored
	ApplyToPluginManagement bool   // also apply this mirror to pluginManagement.repositories
}

// KnownMirrors is the registry of supported mirrors.
// Order matters: entries are applied in the listed order, so URL-based predicates
// (e.g. apache-central) must run before name-based ones that overwrite the URL.
var KnownMirrors = []RepoMirror{ //nolint:gochecknoglobals
	{FlagName: "mavencentral-apache", TemplateID: "ApacheCentral", URLSegment: "apache-central", GradleMatch: `r.getUrl().toString().trimEnd('/').equals("https://repo.maven.apache.org/maven2")`, ApplyToPluginManagement: true},
	{FlagName: "mavencentral", TemplateID: "Central", URLSegment: "central", GradleMatch: `r.getUrl().toString().trimEnd('/').equals("https://repo1.maven.org/maven2")`},
	{FlagName: "google", TemplateID: "Google", URLSegment: "google", GradleMatch: `r.getName().equals("Google")`},
}

//go:embed asset/gradle-mirrors.init.gradle.kts.gotemplate
var gradleMirrorsInitTemplate string

type mirrorTemplateEntry struct {
	ID                      string // unique suffix for Kotlin variable names (e.g. "Central", "Google")
	GradleMatch             string
	MirrorURL               string
	ApplyToPluginManagement bool
}

type gradleMirrorsTemplateData struct {
	Mirrors []mirrorTemplateEntry
}

var activateGradleMirrorsCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "gradle-mirrors",
	Short: "Activate Bitrise repository mirrors for Gradle",
	Long: `Activate Bitrise repository mirrors for Gradle.
This command installs a Gradle init script that redirects repository requests
to Bitrise-hosted mirrors for faster dependency resolution.

Use --mavencentral and/or --google to select specific mirrors.
When no flags are provided, all known mirrors are enabled.

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

		mirrors := enabledMirrors(cmd)

		return ActivateGradleMirrorsFn(logger, gradleHome, utils.AllEnvs(), mirrors)
	},
}

func init() {
	for _, m := range KnownMirrors {
		activateGradleMirrorsCmd.Flags().Bool(m.FlagName, false, "Enable mirror for "+m.FlagName)
	}

	common.ActivateCmd.AddCommand(activateGradleMirrorsCmd)
}

// enabledMirrors returns the mirrors selected by flags.
// If no flag was explicitly set, all known mirrors are returned.
func enabledMirrors(cmd *cobra.Command) []RepoMirror {
	anySet := false
	for _, m := range KnownMirrors {
		if cmd.Flags().Changed(m.FlagName) {
			anySet = true

			break
		}
	}

	if !anySet {
		return KnownMirrors
	}

	var selected []RepoMirror
	for _, m := range KnownMirrors {
		enabled, _ := cmd.Flags().GetBool(m.FlagName)
		if enabled {
			selected = append(selected, m)
		}
	}

	return selected
}

// datacenterToMirrorRegion converts a datacenter env value (e.g. "AMS1", "IAD1", "ORD1")
// to the mirror region slug by lowercasing and stripping trailing digits.
func datacenterToMirrorRegion(dc string) string {
	lower := strings.ToLower(dc)

	return strings.TrimRightFunc(lower, unicode.IsDigit)
}

// ActivateGradleMirrorsFn contains the main logic for the gradle-mirrors command.
func ActivateGradleMirrorsFn(
	logger log.Logger,
	gradleHomePath string,
	envProvider map[string]string,
	mirrors []RepoMirror,
) error {
	enabled := envProvider[gradleMirrorsEnvKey]
	if enabled != "true" {
		logger.Infof("%s is not set to \"true\", skipping Gradle mirror activation", gradleMirrorsEnvKey)

		return nil
	}

	dc := envProvider[datacenterEnvKey]
	if dc == "" {
		logger.Infof("%s is not set, skipping Gradle mirror activation (e.g. local dev environment)", datacenterEnvKey)

		return nil
	}

	if len(mirrors) == 0 {
		logger.Infof("No mirrors selected, skipping Gradle mirror activation")

		return nil
	}

	region := datacenterToMirrorRegion(dc)

	entries := make([]mirrorTemplateEntry, 0, len(mirrors))
	for _, m := range mirrors {
		url := fmt.Sprintf(gradleMirrorURLPattern, region, m.URLSegment)
		entries = append(entries, mirrorTemplateEntry{
			ID:                      m.TemplateID,
			GradleMatch:             m.GradleMatch,
			MirrorURL:               url,
			ApplyToPluginManagement: m.ApplyToPluginManagement,
		})
		logger.Debugf("Mirror %s: region=%s, URL=%s", m.FlagName, region, url)
	}

	tmpl, err := template.New("gradle-mirrors").Parse(gradleMirrorsInitTemplate)
	if err != nil {
		return fmt.Errorf("parse gradle-mirrors init template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, gradleMirrorsTemplateData{Mirrors: entries}); err != nil {
		return fmt.Errorf("execute gradle-mirrors init template: %w", err)
	}

	initDPath := filepath.Join(gradleHomePath, "init.d")
	if err := os.MkdirAll(initDPath, 0o755); err != nil { //nolint:mnd
		return fmt.Errorf("ensure ~/.gradle/init.d exists: %w", err)
	}

	initFilePath := filepath.Join(initDPath, gradleMirrorsInitFile)
	logger.Debugf("Writing Gradle mirrors init script to %s", initFilePath)

	if err := os.WriteFile(initFilePath, buf.Bytes(), 0o644); err != nil { //nolint:gosec,mnd
		return fmt.Errorf("write %s: %w", initFilePath, err)
	}

	logger.Infof("Gradle mirrors activated")

	return nil
}
