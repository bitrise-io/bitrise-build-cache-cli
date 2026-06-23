package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func (d *Doctor) xcelerateProxyCheck() Check {
	pidPath := filepath.Join(d.XcelerateProxyDir(), "proxy.pid")

	return Check{
		Name: "xcelerate-proxy",
		Diagnose: func(_ context.Context) Result {
			content, err := os.ReadFile(pidPath) //nolint:gosec // we control the path
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return Result{State: StateWarn, Detail: "not running (no pid file). Run `bitrise-build-cache xcelerate start-proxy` after `activate` if you use the Xcode cache."}
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

			return Result{State: StateOK, Detail: fmt.Sprintf("running (pid %d)", pid)}
		},
		Fix: func() (string, error) {
			if err := os.Remove(pidPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return "already gone: " + pidPath, nil
				}

				return "", fmt.Errorf("remove %s: %w", pidPath, err)
			}

			return "removed stale " + pidPath, nil
		},
	}
}
