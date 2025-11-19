package common

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

var VersionCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "version",
	Short: "Show the version number of bitrise-build-cache-cli",
	Long:  `Show the version number of bitrise-build-cache-cli`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(common.GetCLIVersion(log.NewLogger()))
	},
}

func init() {
	RootCmd.AddCommand(VersionCmd)
}
