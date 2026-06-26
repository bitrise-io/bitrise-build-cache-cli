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
	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
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
					State:   StateWarn,
					Detail:  fmt.Sprintf("pid %d alive but socket %s not accepting connections (%v) — fixable", pid, socketPath, dialErr),
					Fixable: true,
				}
			}
			_ = conn.Close()

			return Result{State: StateOK, Detail: fmt.Sprintf("running (pid %d, %s)", pid, socketPath)}
		},
		Fix: d.xcelerateProxyFix(pidPath),
	}
}

func (d *Doctor) xcelerateProxyFix(pidPath string) func() (string, error) {
	return func() (string, error) {
		content, err := os.ReadFile(pidPath) //nolint:gosec // path resolved via xceleratconfig helper
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return d.daemonUpFix()
			}

			return "", fmt.Errorf("read %s: %w", pidPath, err)
		}

		pid, perr := strconv.Atoi(strings.TrimSpace(string(content)))
		if perr != nil {
			if err := os.Remove(pidPath); err != nil {
				return "", fmt.Errorf("remove %s: %w", pidPath, err)
			}

			return "removed corrupt " + pidPath, nil
		}

		if err := syscall.Kill(pid, 0); err != nil {
			if err := os.Remove(pidPath); err != nil {
				return "", fmt.Errorf("remove %s: %w", pidPath, err)
			}

			return "removed stale " + pidPath, nil
		}

		return d.daemonRestartFix()
	}
}

//nolint:contextcheck // Check.Fix is ctx-less by design; Background is correct here.
func (d *Doctor) daemonRestartFix() (string, error) {
	restart := d.DaemonRestart
	if restart == nil {
		restart = defaultDaemonRestart
	}

	restarted, err := restart(context.Background())
	if err != nil {
		return "", fmt.Errorf("daemon restart: %w", err)
	}

	if len(restarted) == 0 {
		return "daemon restart: no services touched", nil
	}

	return "restarted: " + strings.Join(restarted, ", "), nil
}

func defaultDaemonRestart(ctx context.Context) ([]string, error) {
	backend, err := daemonpkg.DefaultBackend()
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel
	}

	paths, err := daemonpkg.NewPaths()
	if err != nil {
		return nil, err //nolint:wrapcheck // already context-rich
	}

	if _, err := daemonpkg.Down(ctx, backend, paths, daemonpkg.DefaultServices()); err != nil {
		return nil, err //nolint:wrapcheck // already context-rich
	}

	result, err := daemonpkg.Up(ctx, backend, paths, daemonpkg.DefaultServices())
	if err != nil {
		return nil, err //nolint:wrapcheck // already context-rich
	}

	restarted := make([]string, 0, len(result.Statuses))
	for _, st := range result.Statuses {
		restarted = append(restarted, st.Service.Name)
	}

	return restarted, nil
}
