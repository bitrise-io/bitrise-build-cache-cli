package xcode

import (
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/dsymshim"
)

// dsymutilShimCmd is the on-disk shim's execution entry. The staged
// `~/.bitrise-xcelerate/toolchains/<id>/usr/bin/dsymutil` is a tiny bash
// trampoline that exec's `bitrise-build-cache-cli xcelerate dsymutil-shim --`
// with the original argv so there's a single Go binary to ship.
//
// Hidden from --help: operators never type it; staff do.
var dsymutilShimCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:                "dsymutil-shim [-- dsymutil-args...]",
	Short:              "Internal: dsymutil CAS-plugin shim (invoked by the staged toolchain, not by hand)",
	Hidden:             true,
	DisableFlagParsing: true,
	SilenceUsage:       true,
	SilenceErrors:      true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)

		osProxy := utils.DefaultOsProxy{}
		p, err := paths.Default()
		if err != nil {
			return err //nolint:wrapcheck // thin cmd-layer wrapper
		}

		params := dsymshim.Params{
			Argv:    stripLeadingDashDash(args),
			Env:     utils.AllEnvs(),
			Stdin:   os.Stdin,
			Stdout:  os.Stdout,
			Stderr:  os.Stderr,
			Logger:  logger,
			OsProxy: osProxy,
			Paths:   p,
		}

		exit := params.Run(cmd.Context())
		if exit != 0 {
			os.Exit(exit)
		}

		return nil
	},
}

// stripLeadingDashDash drops the "--" cobra passes through under DisableFlagParsing
// so the shim sees the user's original argv without the separator.
func stripLeadingDashDash(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}

	return args
}

func init() {
	xcelerateCommand.AddCommand(dsymutilShimCmd)
}
