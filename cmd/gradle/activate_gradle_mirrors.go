package gradle

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	mirrorsconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/gradle/mirrors"
	mirrorspkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/gradle/mirrors"
)

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
		activator := mirrorspkg.NewActivator(mirrorspkg.ActivatorParams{
			GradleHome:    gradleHomeNonExpanded,
			SelectedFlags: selectedMirrorFlags(cmd),
			DebugLogging:  common.IsDebugLogMode,
		})

		return activator.Activate(cmd.Context())
	},
}

func init() {
	for _, m := range mirrorsconfig.KnownMirrors {
		activateGradleMirrorsCmd.Flags().Bool(m.FlagName, false, "Enable mirror for "+m.FlagName)
	}

	common.ActivateCmd.AddCommand(activateGradleMirrorsCmd)
}

// selectedMirrorFlags returns the flag names selected on the command. When no
// flag is set, an empty slice is returned, which the activator treats as
// "enable all known mirrors".
func selectedMirrorFlags(cmd *cobra.Command) []string {
	var selected []string

	for _, m := range mirrorsconfig.KnownMirrors {
		if !cmd.Flags().Changed(m.FlagName) {
			continue
		}

		enabled, _ := cmd.Flags().GetBool(m.FlagName)
		if enabled {
			selected = append(selected, m.FlagName)
		}
	}

	return selected
}
