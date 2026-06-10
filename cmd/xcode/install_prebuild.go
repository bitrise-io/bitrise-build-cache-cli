package xcode

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcodescheme"
)

//nolint:gochecknoglobals
var (
	installPrebuildProject string
	installPrebuildScheme  string
	installPrebuildRemove  bool
)

//nolint:gochecknoglobals
var installPrebuildCmd = &cobra.Command{
	Use:          "install-prebuild",
	Short:        "Install a Bitrise Build Cache health check as a pre-build Xcode scheme action",
	Long:         `Modifies the shared .xcscheme XML inside an .xcodeproj so that hitting ⌘B in Xcode runs ` + "`bitrise-build-cache doctor`" + ` before swiftc. Idempotent. Pass --remove to undo.`,
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		project := strings.TrimSuffix(installPrebuildProject, "/")
		path, err := xcodescheme.ResolveSchemePath(project, installPrebuildScheme)
		if err != nil {
			return fmt.Errorf("locate scheme file: %w", err)
		}

		if installPrebuildRemove {
			return runUninstall(logger, path)
		}

		return runInstall(logger, path)
	},
}

func runInstall(logger log.Logger, path string) error {
	status, err := xcodescheme.Install(path)
	if err != nil {
		return fmt.Errorf("install pre-build action: %w", err)
	}

	switch status {
	case xcodescheme.StatusInstalled:
		logger.TInfof("✅ Installed Bitrise Build Cache pre-build action in %s", path)
		logger.Infof("Restart Xcode for the change to take effect for an already-open project.")
	case xcodescheme.StatusAlreadyInstalled:
		logger.TInfof("Already installed in %s — no changes made", path)
	case xcodescheme.StatusUninstalled, xcodescheme.StatusNotInstalled:
		// Not reachable from Install but enumerated for exhaustiveness.
		logger.Warnf("unexpected status %v from Install", status)
	}

	return nil
}

func runUninstall(logger log.Logger, path string) error {
	status, err := xcodescheme.Uninstall(path)
	if err != nil {
		return fmt.Errorf("uninstall pre-build action: %w", err)
	}

	switch status {
	case xcodescheme.StatusUninstalled:
		logger.TInfof("✅ Removed Bitrise Build Cache pre-build action from %s", path)
	case xcodescheme.StatusNotInstalled:
		logger.TInfof("Nothing to do — no Bitrise Build Cache pre-build action found in %s", path)
	case xcodescheme.StatusInstalled, xcodescheme.StatusAlreadyInstalled:
		logger.Warnf("unexpected status %v from Uninstall", status)
	}

	return nil
}

func init() {
	installPrebuildCmd.Flags().StringVar(&installPrebuildProject, "project", "",
		"Path to the .xcodeproj (e.g. ~/dev/MyApp/MyApp.xcodeproj) (required)")
	installPrebuildCmd.Flags().StringVar(&installPrebuildScheme, "scheme", "",
		"Name of the scheme to modify (e.g. MyApp) (required)")
	installPrebuildCmd.Flags().BoolVar(&installPrebuildRemove, "remove", false,
		"Remove the pre-build action instead of installing it")
	_ = installPrebuildCmd.MarkFlagRequired("project")
	_ = installPrebuildCmd.MarkFlagRequired("scheme")

	common.RootCmd.AddCommand(installPrebuildCmd)
}
