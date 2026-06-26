package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

func (d *Doctor) ccacheHelperCheck() Check {
	socketPath := ccacheconfig.ResolveIPCSocketPath("", d.Envs, utils.DefaultOsProxy{})

	return Check{
		Name: "ccache-helper",
		Diagnose: func(ctx context.Context) Result {
			if !d.toolActivated(toolconfig.Ccache) {
				return Result{State: StateOK, Detail: "skipped (c++ not activated)"}
			}

			if _, err := os.Stat(socketPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return Result{
						State:   StateWarn,
						Detail:  "not running (no socket file). Run `bitrise-build-cache daemon up` (or `ccache start-storage-helper` if no daemon is installed).",
						Fixable: true,
						Fixer:   DaemonUpFixer{},
					}
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
					Fixer:   RemoveFileFixer{Path: socketPath, Label: "orphan socket"},
				}
			}
			_ = conn.Close()

			return Result{State: StateOK, Detail: "running (" + socketPath + ")"}
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
