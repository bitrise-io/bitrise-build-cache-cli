package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
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
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
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

var rbeEnabled bool //nolint:gochecknoglobals
var timestamps bool //nolint:gochecknoglobals

func init() {
	enableForBazelCmd.Flags().BoolVar(&rbeEnabled, "with-rbe", false, "Enable Remote Build Execution (RBE)")
	enableForBazelCmd.Flags().BoolVar(&timestamps, "timestamps", false, "Enable timestamps in build output")
	enableForCmd.AddCommand(enableForBazelCmd)
}

func enableForBazelCmdFn(logger log.Logger, homeDirPath string, envProvider func(string) string) error {
	logger.Infof("(i) Checking parameters")

	// Required configs
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environments: %w", err)
	}

	// Optional configs
	// CacheEndpointURL
	cacheEndpointURL := common.SelectCacheEndpointURL(envProvider("BITRISE_BUILD_CACHE_ENDPOINT"), envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", cacheEndpointURL)
	// RBEEndpointURL
	var rbeEndpointURL string
	if rbeEnabled {
		rbeEndpointURL = common.SelectRBEEndpointURL(envProvider("BITRISE_RBE_ENDPOINT"), envProvider)
		if rbeEndpointURL != "" {
			logger.Infof("(i) RBE Endpoint URL: %s", rbeEndpointURL)
		} else {
			logger.Infof("(i) RBE is not available at this location")
		}
	}
	// Metadata
	cacheConfig := common.NewCacheConfigMetadata(os.Getenv,
		func(name string, v ...string) (string, error) {
			output, err := exec.Command(name, v...).Output()

			return string(output), err
		},
		logger)
	logger.Infof("(i) Cache Config: %+v", cacheConfig)

	logger.Infof("(i) Check ~/.bazelrc")
	bazelrcPath, err := pathutil.NewPathModifier().AbsPath(filepath.Join(homeDirPath, ".bazelrc"))
	if err != nil {
		return fmt.Errorf("get absolute path of ~/.bazelrc, error: %w", err)
	}
	currentBazelrcFileContent, isBazelrcExists, err := readFileIfExists(bazelrcPath)
	if err != nil {
		return fmt.Errorf("check if ~/.bazelrc exists at %s, error: %w", bazelrcPath, err)
	}
	logger.Debugf("isBazelrcExists: %t", isBazelrcExists)

	logger.Infof("(i) Generate ~/.bazelrc")
	bazelrcBlockContent, err := bazelconfig.GenerateBazelrc(cacheEndpointURL,
		authConfig.WorkspaceID, authConfig.AuthToken, cacheConfig,
		bazelconfig.Preferences{
			RBEEndpointURL:      rbeEndpointURL,
			IsTimestampsEnabled: timestamps,
		})
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
	err = os.WriteFile(bazelrcPath, []byte(bazelrcContent), 0755) //nolint:gosec,gomnd,mnd
	if err != nil {
		return fmt.Errorf("write bazelrc config to %s, error: %w", bazelrcPath, err)
	}

	return nil
}
