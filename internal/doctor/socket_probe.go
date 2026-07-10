package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

// socketDaemonCheck returns a Check that reports "running" iff `socketPath`
// exists and accepts a unix-domain connection within probeTimeout. The check
// short-circuits to StateOK when the referenced tool isn't activated.
func (d *Doctor) socketDaemonCheck(name string, tool toolconfig.Tool, toolLabel, socketPath string) Check {
	return Check{
		Name: name,
		Diagnose: func(ctx context.Context) Result {
			if !d.toolActivated(tool) {
				return Result{State: StateOK, Detail: "skipped (" + toolLabel + " not activated)"}
			}

			if _, err := os.Stat(socketPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return Result{
						State:   StateWarn,
						Detail:  "not running (no socket file)",
						Fixable: true,
						Fixer:   DaemonUpFixer{},
					}
				}

				return Result{State: StateError, Detail: fmt.Sprintf("stat %s socket: %s", name, err.Error())}
			}

			dialer := &net.Dialer{Timeout: probeSocketTimeout}
			probeCtx, cancel := context.WithTimeout(ctx, probeSocketTimeout)
			defer cancel()
			conn, dialErr := dialer.DialContext(probeCtx, "unix", socketPath)
			if dialErr != nil {
				return Result{
					State:   StateWarn,
					Detail:  fmt.Sprintf("stuck: socket %s present but not accepting connections (%v) — fixable", socketPath, dialErr),
					Fixable: true,
					Fixer:   DaemonRestartFixer{},
				}
			}
			_ = conn.Close()

			return Result{State: StateOK, Detail: "running (" + socketPath + ")"}
		},
	}
}

const probeSocketTimeout = 500 * time.Millisecond
