package cmd

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

// This command should go under an xcelerate subcommand together with stop-xcode-proxy IMO
var stopXcelerateProxyCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:          "stop-xcode-proxy",
	Short:        "TBD",
	Long:         `TBD`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof(activateXcode)

		return stopXcelerateProxyCommandFn(logger)
	},
}

func init() {
	rootCmd.AddCommand(stopXcelerateProxyCmd)
}

func stopXcelerateProxyCommandFn(
	logger log.Logger,
) error {
	pidPath := xcelerate.XceleratePathFor(pidFile)

	b, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("read pidfile: %w", err)
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		return fmt.Errorf("bad pid: %w", err)
	}

	// Send SIGTERM to the process group: negative PID means group in unix kill
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		logger.Debugf("kill (TERM) failed: %w", err)
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
			// check existence with kill(pid, 0)
			if innerErr := syscall.Kill(pid, 0); innerErr != nil {
				// ESRCH => no such process
				break loop
			}
		}
	}

	// If still alive, escalate to SIGKILL
	_ = syscall.Kill(-pid, syscall.SIGKILL)

	// remove pidfile (ignore errors)
	_ = os.Remove(pidPath)
	logger.TDonef("Stopped xcelerate-proxy")

	return nil //nolint:nilerr
}
