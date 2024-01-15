package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/bazel"
	cacheconfigcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/stringmerge"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		//
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Enable Bitrise Build Cache for Bazel")
		//
		bazelHomeDirPath, err := pathutil.NewPathModifier().AbsPath("~")
		if err != nil {
			return fmt.Errorf("expand Bazel home path, error: %w", err)
		}

		if err := enableForBazelCmdFn(logger, bazelHomeDirPath, os.Getenv); err != nil {
			return fmt.Errorf("enable Bazel Build Cache: %w", err)
		}

		logger.TInfof("✅ Bitrise Build Cache for Bazel enabled")

		return nil
	},
}

func init() {
	enableForCmd.AddCommand(enableForBazelCmd)
}

func enableForBazelCmdFn(logger log.Logger, homeDirPath string, envProvider func(string) string) error {
	bazelrcPath, err := pathutil.NewPathModifier().AbsPath(filepath.Join(homeDirPath, ".bazelrc"))
	if err != nil {
		return fmt.Errorf("get absolute path of ~/.bazelrc, error: %w", err)
	}

	logger.Infof("(i) Checking parameters")
	endpointURL := cacheconfigcommon.SelectEndpointURL(envProvider("BITRISE_BUILD_CACHE_ENDPOINT"), envProvider)
	ciProvider := envProvider("CI_PROVIDER")

	authConfig, err := cacheconfigcommon.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environments: %w", err)
	}

	logger.Infof("(i) Check ~/.bazelrc")
	currentBazelrcFileContent, isBazelrcExists, err := readFileIfExists(bazelrcPath)
	if err != nil {
		return fmt.Errorf("check if ~/.bazelrc exists at %s, error: %w", bazelrcPath, err)
	}
	logger.Debugf("isBazelrcExists: %t", isBazelrcExists)

	logger.Infof("(i) Generate ~/.bazelrc")
	bazelrcBlockContent, err := bazelconfig.GenerateBazelrc(endpointURL, authConfig.WorkspaceID, authConfig.AuthToken, ciProvider)
	if err != nil {
		return fmt.Errorf("generate bazelrc: %w", err)
	}

	bazelrcContent := stringmerge.ChangeContentInBlock(
		currentBazelrcFileContent,
		"# [start] generated-by-bitrise-build-cache",
		"# [end] generated-by-bitrise-build-cache",
		bazelrcBlockContent,
	)

	logger.Infof("(i) Writing config into ~/.bazelrc")
	err = os.WriteFile(bazelrcPath, []byte(bazelrcContent), 0755) //nolint:gosec,gomnd
	if err != nil {
		return fmt.Errorf("write bazelrc config to %s, error: %w", bazelrcPath, err)
	}

	return nil
}
