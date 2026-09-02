package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/clibin"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// ResolveSupervisedBinary returns the CLI path safe to pin into a supervisor
// config, copying the running binary out of a transient location if needed.
func ResolveSupervisedBinary(logger log.Logger) (string, error) {
	// Do NOT EvalSymlinks — embedding the symlinked path lets CLI upgrades land without rerunning install.
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve CLI executable path: %w", err)
	}

	if clibin.IsTransientPath(exe) {
		stable, copyErr := clibin.CopyToStable(exe)
		if copyErr != nil {
			return "", fmt.Errorf("copy CLI to stable dir before daemon install: %w", copyErr)
		}

		logger.Donef("Copied CLI binary to %s (was on a transient path: %s)", stable, exe)
		exe = stable
	}

	WarnIfShadowedBinary(logger, exe)

	return exe, nil
}

// WarnIfShadowedBinary flags the supervisor pinning a different binary than $PATH resolves to.
func WarnIfShadowedBinary(logger log.Logger, pinned string) {
	onPath, err := exec.LookPath(paths.CLIBinaryName)
	if err != nil {
		return
	}
	if resolvePath(pinned) == resolvePath(onPath) {
		return
	}

	logger.Warnf("Pinning %s into the supervisor config, but `%s` on your $PATH resolves to %s.", pinned, paths.CLIBinaryName, onPath)
	logger.Warnf("Interactive commands and the daemon would use different binaries — likely different CLI versions too.")
	logger.Warnf("To pin the PATH binary instead, rerun via its full path: %s daemon install", onPath)
}

func resolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}

	return resolved
}

// Bootstrap registers services with the OS supervisor and starts them, resolving
// the backend, paths and CLI binary itself. Install already starts what it
// writes, so this is install-only by design.
func Bootstrap(ctx context.Context, logger log.Logger, services []Service) (InstallResult, Paths, error) {
	backend, dPaths, err := DefaultBackendAndPaths()
	if err != nil {
		return InstallResult{}, Paths{}, err
	}

	exe, err := ResolveSupervisedBinary(logger)
	if err != nil {
		return InstallResult{}, dPaths, err
	}

	result, err := Install(ctx, backend, dPaths, services, exe)
	if err != nil {
		return result, dPaths, fmt.Errorf("install daemon: %w", err)
	}

	return result, dPaths, nil
}

func DefaultBackendAndPaths() (Backend, Paths, error) {
	backend, err := DefaultBackend()
	if err != nil {
		return nil, Paths{}, err //nolint:wrapcheck // sentinel
	}

	dPaths, err := NewPaths()
	if err != nil {
		return nil, Paths{}, err //nolint:wrapcheck // already context-rich
	}

	return backend, dPaths, nil
}

// ServicesForTools reports which services activation should supervise, which
// is none of them.
//
// The xcelerate proxy is measured: macOS applies CPU and I/O limits per
// resource coalition and places a launchd job in one of its own, so the proxy
// competes with the compiler it exists to serve and loses — 2314ms against
// 6.3ms per cache operation on a 4-core machine, with no plist key able to
// close it.
//
// The ccache helper is **not** measured as harmful. The same A/B found no
// penalty for it (3.8ms supervised against 2.4ms lazy at p50, with better
// tails supervised). It is unsupervised anyway, on every platform, for
// consistency and out of caution: it has the same shape as the proxy, that
// test ruled out a large effect rather than a small one, and one lifecycle is
// easier to reason about than two. The same caution covers Linux, where
// coalitions do not exist and nothing was measured at all.
//
// Both are started by whoever needs them — the xcodebuild wrapper for the
// proxy, `activate c++` for the helper — which puts them in the build's
// coalition. `daemon install` still supervises on request, and warns.
//
// See docs/daemon-latency.md.
func ServicesForTools(_, _ bool) []Service { return nil }
