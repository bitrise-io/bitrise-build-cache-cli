package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

func (d *Doctor) xcelerateProxyCheck() Check {
	osProxy := utils.DefaultOsProxy{}
	pidPath := xceleratconfig.ProxyPidFile(osProxy)
	socketPath := xceleratconfig.ResolveProxySocketPath("", d.Envs, osProxy)

	return Check{
		Name: "xcelerate-proxy",
		Diagnose: func(ctx context.Context) Result {
			if !d.toolActivated(toolconfig.Xcelerate) {
				return Result{State: StateOK, Detail: "skipped (xcode not activated)"}
			}

			content, err := os.ReadFile(pidPath) //nolint:gosec // path resolved via xceleratconfig helper
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return Result{
						State:   StateWarn,
						Detail:  "not running (no pid file). Run `bitrise-build-cache daemon up` (or `xcelerate start-proxy` if no daemon is installed).",
						Fixable: true,
					}
				}

				return Result{State: StateError, Detail: "read pid file: " + err.Error()}
			}

			pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
			if err != nil {
				return Result{State: StateWarn, Detail: "pid file content invalid (" + err.Error() + ") — fixable", Fixable: true}
			}

			if err := syscall.Kill(pid, 0); err != nil {
				return Result{
					State:   StateWarn,
					Detail:  fmt.Sprintf("stale pid file: pid %d not running — fixable", pid),
					Fixable: true,
				}
			}

			dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
			probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer cancel()
			conn, dialErr := dialer.DialContext(probeCtx, "unix", socketPath)
			if dialErr != nil {
				return Result{
					State:  StateWarn,
					Detail: fmt.Sprintf("pid %d alive but socket %s not accepting connections (%v)", pid, socketPath, dialErr),
				}
			}
			_ = conn.Close()

			return Result{State: StateOK, Detail: fmt.Sprintf("running (pid %d, %s)", pid, socketPath)}
		},
		Fix: func() (string, error) {
			if _, err := os.Stat(pidPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return d.daemonUpFix()
				}

				return "", fmt.Errorf("stat %s: %w", pidPath, err)
			}

			if err := os.Remove(pidPath); err != nil {
				return "", fmt.Errorf("remove %s: %w", pidPath, err)
			}

			return "removed stale " + pidPath, nil
		},
	}
}
