package bazel

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

//nolint:gochecknoglobals
var (
	installShimDir     string
	installShimVersion string
)

// cliVersionPattern restricts what may be templated into the shim script body.
//
//nolint:gochecknoglobals
var cliVersionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

//nolint:gochecknoglobals
var installCredHelperShimCmd = &cobra.Command{
	Use:   "install-credhelper-shim",
	Short: "Write the credential-helper shim script into the workspace",
	Long: `Write ` + paths.BazelCredHelperShimName + ` into the workspace at --dir (default
` + paths.BazelCredHelperShimDefaultDir + `). Commit both the shim and the .bazelrc line printed by
` + "`" + `print-credhelper-line` + "`" + `.

The shim auto-installs bitrise-build-cache into a workspace-local cache when
the CLI is missing from PATH, so machines without a preinstalled CLI still
succeed on the first ` + "`" + `bazel build` + "`" + `.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		dir := installShimDir
		if dir == "" {
			dir = paths.BazelCredHelperShimDefaultDir
		}

		version := installShimVersion
		if version == "" {
			version = configcommon.GetCLIVersion(logger)
		}
		if version != "" && !cliVersionPattern.MatchString(version) {
			return fmt.Errorf("invalid --cli-version %q: expected [A-Za-z0-9._-]+", version)
		}

		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("resolve --dir: %w", err)
		}
		if err := os.MkdirAll(absDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", absDir, err)
		}

		target := filepath.Join(absDir, paths.BazelCredHelperShimName)
		body := bazelconfig.RenderCredHelperShim(version)
		if err := os.WriteFile(target, []byte(body), 0o755); err != nil { //nolint:gosec // shim needs to be executable
			return fmt.Errorf("write %s: %w", target, err)
		}

		logger.TInfof("Wrote credential-helper shim to %s", target)
		logger.Infof("Add this line to your repo .bazelrc:")
		if filepath.IsAbs(dir) {
			logger.Infof("  build --credential_helper=*.services.bitrise.io=%s", filepath.ToSlash(target))
		} else {
			logger.Infof("  build --credential_helper=*.services.bitrise.io=%%workspace%%/%s/%s",
				filepath.ToSlash(dir), paths.BazelCredHelperShimName)
		}

		return nil
	},
}

func init() {
	installCredHelperShimCmd.Flags().StringVar(&installShimDir, "dir", paths.BazelCredHelperShimDefaultDir,
		"Workspace-relative dir to write the shim into.")
	installCredHelperShimCmd.Flags().StringVar(&installShimVersion, "cli-version", "",
		"CLI version the shim pins for auto-install (defaults to the running CLI version).")
	bazelCmd.AddCommand(installCredHelperShimCmd)
}
