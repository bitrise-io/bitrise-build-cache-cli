package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

// enableForBazelCmd represents the bazel command
var enableForBazelCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "bazel",
	Short: "Enable Bitrise Build Cache for Bazel",
	Long: `Enable Bitrise Build Cache for Bazel.
This command will:

Create a ~/.bazelrc file with the necessary configs.
If the file doesn't exist it will be created.
If it already exists a "# [start/end] generated-by-bitrise-build-cache" block will be added to the end of the file.
If the "# [start/end] generated-by-bitrise-build-cache" block is already present in the file then only the block's content will be modified.
`,
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		//
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Enable Bitrise Build Cache for Bazel")
		//

		allEnvs := utils.AllEnvs()
		if err := EnableForBazelCmdFn(logger, utils.DefaultOsProxy{}, allEnvs); err != nil {
			return fmt.Errorf("enable Bazel Build Cache: %w", err)
		}

		logger.TInfof("✅ Bitrise Build Cache for Bazel enabled")

		return nil
	},
}

var rbeEnabled bool //nolint:gochecknoglobals
var timestamps bool //nolint:gochecknoglobals

func init() {
	enableForBazelCmd.Flags().BoolVar(&rbeEnabled, "with-rbe", false, "Enable Remote Build Execution (RBE)")
	enableForBazelCmd.Flags().BoolVar(&timestamps, "timestamps", false, "Enable timestamps in build output")
	enableForCmd.AddCommand(enableForBazelCmd)
}

func EnableForBazelCmdFn(logger log.Logger, osProxy utils.OsProxy, envProvider map[string]string) error {
	logger.Infof("(i) Checking parameters")

	// CacheConfigMetadata
	cacheConfig := common.NewMetadata(utils.AllEnvs(),
		func(name string, v ...string) (string, error) {
			output, err := exec.Command(name, v...).Output() //nolint:noctx

			return string(output), err
		},
		logger)
	logger.Infof("(i) Cache Config: %+v", cacheConfig)

	logger.Infof("(i) Check ~/.bazelrc")

	userHomeDir, err := osProxy.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get user home directory, error: %w", err)
	}

	bazelrcPath, err := pathutil.NewPathModifier().AbsPath(filepath.Join(userHomeDir, ".bazelrc"))
	if err != nil {
		return fmt.Errorf("get absolute path of ~/.bazelrc, error: %w", err)
	}

	logger.Infof("(i) Generate ~/.bazelrc")
	params := bazelconfig.DefaultActivateBazelParams()
	params.Cache.Enabled = true
	params.Cache.PushEnabled = true
	params.RBE.Enabled = rbeEnabled
	params.Timestamps = timestamps

	inventory, err := params.TemplateInventory(logger, envProvider, func(cmd string, params ...string) (string, error) {
		output, err2 := exec.Command(cmd, params...).CombinedOutput() //nolint:noctx
		if err2 == nil {
			return string(output), nil
		}

		return string(output), fmt.Errorf("run cmd: %w", err2)
	}, isDebugLogMode)
	if err != nil {
		return fmt.Errorf("template inventory error: %w", err)
	}

	bazelrcBlockContent, err := inventory.GenerateBazelrc(utils.DefaultTemplateProxy())
	if err != nil {
		return fmt.Errorf("generate bazelrc: %w", err)
	}

	logger.Infof("(i) Writing config into ~/.bazelrc")
	err = AddContentOrCreateFile(logger, osProxy, bazelrcPath, "generated-by-bitrise-build-cache", bazelrcBlockContent)
	if err != nil {
		return fmt.Errorf("add content to ~/.bazelrc, error: %w", err)
	}

	return nil
}
