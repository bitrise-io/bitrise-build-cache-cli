package cmd

import (
	"fmt"
	"path/filepath"

	"os/exec"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

// activateBazelCmd represents the activate bazel command

var activateBazelCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "bazel",
	Short: "Activates Bazel remote cache",
	Long: `Activates Bazel remote cache by creating/updating the .bazelrc file in your home directory.

This command will:
- Create a ~/.bazelrc file with the necessary configs if it doesn't exist
- If it exists, add a "# [start] generated-by-bitrise-build-cache" block to the end of the file
- If the configuration block already exists, only update its content

The command supports:
- Remote caching with push/pull capabilities
- Build Event Service (BES) integration
- Remote Build Execution (RBE)`,
	RunE:         activateBazel,
	SilenceUsage: true,
}

//nolint:gochecknoglobals
var activateBazelParams = bazelconfig.DefaultActivateBazelParams()

func init() {
	activateCmd.AddCommand(activateBazelCmd)

	flags := activateBazelCmd.Flags()
	flags.BoolVar(&activateBazelParams.Cache.Enabled, "cache", activateBazelParams.Cache.Enabled, "Enable remote cache")
	flags.BoolVar(&activateBazelParams.Cache.PushEnabled, "cache-push", activateBazelParams.Cache.PushEnabled, "Enable pushing new cache entries")
	flags.StringVar(&activateBazelParams.Cache.Endpoint, "cache-endpoint", activateBazelParams.Cache.Endpoint, "Remote cache endpoint URL")
	flags.BoolVar(&activateBazelParams.BES.Enabled, "bes", activateBazelParams.BES.Enabled, "Enable Build Event Service (BES)")
	flags.StringVar(&activateBazelParams.BES.Endpoint, "bes-endpoint", activateBazelParams.BES.Endpoint, "BES endpoint URL")
	flags.BoolVar(&activateBazelParams.RBE.Enabled, "rbe", activateBazelParams.RBE.Enabled, "Enable Remote Build Execution (RBE)")
	flags.StringVar(&activateBazelParams.RBE.Endpoint, "rbe-endpoint", activateBazelParams.RBE.Endpoint, "RBE endpoint URL")
	flags.BoolVar(&activateBazelParams.Timestamps, "timestamps", activateBazelParams.Timestamps, "Enable timestamps in build output")
}

func activateBazel(_ *cobra.Command, _ []string) error {
	logger := log.NewLogger()
	logger.EnableDebugLog(isDebugLogMode)
	logger.TInfof("Activate Bitrise Build Cache for Bazel")

	// Get bazelrc path
	homeDir, err := pathutil.NewPathModifier().AbsPath("~")
	if err != nil {
		return fmt.Errorf("expand home path, error: %w", err)
	}
	bazelrcPath := filepath.Join(homeDir, ".bazelrc")

	// Run main logic
	if err := ActivateBazelCmdFn(
		logger,
		bazelrcPath,
		utils.AllEnvs(),
		func(cmd string, params ...string) (string, error) {
			output, err2 := exec.Command(cmd, params...).CombinedOutput() //nolint:noctx
			if err2 == nil {
				return string(output), nil
			}

			return string(output), fmt.Errorf("run cmd: %w", err2)
		},
		activateBazelParams.TemplateInventory,
		func(inventory bazelconfig.TemplateInventory, path string) error {
			return inventory.WriteToBazelrc(logger, path, utils.DefaultOsProxy{}, utils.DefaultTemplateProxy())
		},
	); err != nil {
		return fmt.Errorf("activate Bazel Build Cache: %w", err)
	}

	logger.TInfof("✅ Bitrise Build Cache activated for Bazel")

	return nil
}

func ActivateBazelCmdFn(
	logger log.Logger,
	bazelrcPath string,
	envs map[string]string,
	commandFunc common.CommandFunc,
	templateInventoryProvider func(log.Logger, map[string]string, common.CommandFunc, bool) (bazelconfig.TemplateInventory, error),
	templateWriter func(bazelconfig.TemplateInventory, string) error,
) error {
	// Generate template inventory
	inventory, err := templateInventoryProvider(logger, envs, commandFunc, isDebugLogMode)
	if err != nil {
		return err
	}

	// Write to bazelrc
	if err := templateWriter(inventory, bazelrcPath); err != nil {
		return err
	}

	return nil
}
