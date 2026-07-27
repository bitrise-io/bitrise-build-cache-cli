package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// transientBinPrefixes mark filesystem locations whose contents the OS may prune between logins;
// embedding such a path in a LaunchAgent/systemd unit would leave the supervisor pointing at a missing binary.
//
//nolint:gochecknoglobals
var transientBinPrefixes = []string{
	"/tmp/",
	"/var/folders/",
	"/private/var/folders/",
	"/private/tmp/",
}

func IsTransientBinaryPath(exe string) bool {
	for _, prefix := range transientBinPrefixes {
		if strings.HasPrefix(exe, prefix) {
			return true
		}
	}

	return false
}

// ResolveSupervisedBinary returns the CLI path safe to pin into a supervisor
// config, copying the running binary out of a transient location if needed.
func ResolveSupervisedBinary(logger log.Logger) (string, error) {
	// Do NOT EvalSymlinks — embedding the symlinked path lets CLI upgrades land without rerunning install.
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve CLI executable path: %w", err)
	}

	if IsTransientBinaryPath(exe) {
		stable, copyErr := CopyCLIToStableBin(exe)
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

// ServicesForTools filters DefaultServices() down to what the given tools need;
// tools with no background service (Gradle, Bazel) contribute none.
func ServicesForTools(needsXcelerate, needsCcache bool) []Service {
	var out []Service
	for _, svc := range DefaultServices() {
		switch svc.Name {
		case ServiceXcelerateProxy:
			if needsXcelerate {
				out = append(out, svc)
			}
		case ServiceCcacheHelper:
			if needsCcache {
				out = append(out, svc)
			}
		}
	}

	return out
}
