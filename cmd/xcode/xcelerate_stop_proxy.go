package xcode

import (
	"fmt"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// This command should go under an xcelerate subcommand together with stop-xcode-proxy IMO
var stopXcelerateProxyCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:          "stop-proxy",
	Short:        "TBD",
	Long:         `TBD`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)

		return stopXcelerateProxyCommandFn(utils.DefaultOsProxy{}, logger)
	},
}

func init() {
	xcelerateCommand.AddCommand(stopXcelerateProxyCmd)
}

func stopXcelerateProxyCommandFn(osProxy utils.OsProxy, logger log.Logger) error {
	logger.TInfof("Stopping xcelerate-proxy...")

	pid, running := proxyOwner(osProxy)
	if !running {
		logger.TDonef("No xcelerate-proxy is running")

		return nil
	}
	if pid <= 0 {
		return fmt.Errorf("a proxy holds %s but advertised no usable pid", proxyPidFile(osProxy))
	}

	// Send SIGTERM to the process group: negative PID means group in unix kill
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		logger.Debugf("kill (TERM) failed: %s", err)
		// maybe the process is already gone; continue to remove pidfile.
	}

	// wait up to N seconds for process to exit
	timeout := time.After(5 * time.Second)
	tick := time.Tick(200 * time.Millisecond)
loop:
	for {
		select {
		case <-timeout:
			break loop
		case <-tick:
			// check existence
			if innerErr := syscall.Kill(-pid, 0); innerErr != nil {
				// ESRCH => no such process
				break loop
			}
		}
	}

	// If still alive, escalate to SIGKILL
	_ = syscall.Kill(-pid, syscall.SIGKILL)

	// The pid file is not removed: it carries the lock, and unlinking it would let a
	// proxy holding the old inode and one locking a newly created file both run. The
	// kernel drops the lock when the process dies, which is what frees it.
	logger.TDonef("Stopped xcelerate-proxy")

	return nil //nolint:nilerr
}
