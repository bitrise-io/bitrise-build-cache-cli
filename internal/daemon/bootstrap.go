package daemon

import (
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
