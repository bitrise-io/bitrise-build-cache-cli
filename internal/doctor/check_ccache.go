package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"
)

func (d *Doctor) ccacheHelperCheck() Check {
	return Check{
		Name: "ccache-helper",
		Diagnose: func(ctx context.Context) Result {
			socketPath := d.Envs["BITRISE_CCACHE_IPC_SOCKET_PATH"]
			if socketPath == "" {
				socketPath = filepath.Join(os.TempDir(), "ccache-ipc.sock")
			}

			if _, err := os.Stat(socketPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return Result{State: StateWarn, Detail: "not running (no socket file). Run `bitrise-build-cache ccache start-storage-helper` if you build C/C++."}
				}

				return Result{State: StateError, Detail: "stat ccache socket: " + err.Error()}
			}

			dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
			probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer cancel()
			conn, err := dialer.DialContext(probeCtx, "unix", socketPath)
			if err != nil {
				return Result{
					State:   StateWarn,
					Detail:  fmt.Sprintf("socket %s present but not accepting connections (%v) — fixable", socketPath, err),
					Fixable: true,
				}
			}
			_ = conn.Close()

			return Result{State: StateOK, Detail: "running (" + socketPath + ")"}
		},
		Fix: func() (string, error) {
			socketPath := d.Envs["BITRISE_CCACHE_IPC_SOCKET_PATH"]
			if socketPath == "" {
				socketPath = filepath.Join(os.TempDir(), "ccache-ipc.sock")
			}

			if err := os.Remove(socketPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return "already gone: " + socketPath, nil
				}

				return "", fmt.Errorf("remove %s: %w", socketPath, err)
			}

			return "removed orphan socket " + socketPath, nil
		},
	}
}

func (d *Doctor) ccacheBinaryCheck() Check {
	return Check{
		Name: "ccache-binary",
		Diagnose: func(_ context.Context) Result {
			path, err := d.LookPath("ccache")
			if err != nil {
				return Result{State: StateWarn, Detail: "ccache binary not found in PATH. Install via `brew install ccache` if you build C/C++."}
			}

			return Result{State: StateOK, Detail: "found at " + path}
		},
	}
}
