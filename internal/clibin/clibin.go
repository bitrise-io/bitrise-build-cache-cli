// Package clibin locates a copy of the CLI that other processes can still run
// later: build tools call back into it (the Bazel credential helper, the Gradle
// token resolver) and supervisors pin it into their configs, so the path baked
// into a generated file has to outlive the command that wrote it.
package clibin

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// transientPrefixes mark locations whose contents the OS may prune, or that
// disappear with the process that created them — `go run` builds into
// $TMPDIR/go-build*/ and deletes it on exit, and an unpacked tarball under /tmp
// goes the same way.
//
//nolint:gochecknoglobals
var transientPrefixes = []string{
	"/tmp/",
	"/var/folders/",
	"/private/var/folders/",
	"/private/tmp/",
}

func IsTransientPath(exe string) bool {
	for _, prefix := range transientPrefixes {
		if strings.HasPrefix(exe, prefix) {
			return true
		}
	}

	return false
}

// StablePath returns ~/.bitrise/bin/bitrise-build-cache.
func StablePath() (string, error) {
	p, err := paths.Default()
	if err != nil {
		return "", fmt.Errorf("resolve stable bin path: %w", err)
	}

	return p.BitriseBinFile(paths.CLIBinaryName), nil
}

// TransientPolicy is what to do when the running CLI sits on a transient path.
type TransientPolicy int

const (
	// CopyToStableDir copies the binary somewhere permanent and pins that path.
	// For configs that need a real path, like Bazel's --credential_helper.
	CopyToStableDir TransientPolicy = iota
	// PreferPATH resolves to "" so the caller falls back to the bare binary name
	// and $PATH does the lookup at call time. Survives CLI upgrades, and is right
	// wherever the consumer can run a bare command name.
	PreferPATH
)

// Resolve returns the CLI path to embed in a generated config. The running
// executable is used as-is unless it sits somewhere transient — a path from
// `go run` is gone by the time a build tool calls it — in which case policy
// decides. Returns "" when the caller should fall back to the bare name.
func Resolve(logger log.Logger, policy TransientPolicy) string {
	// Do NOT EvalSymlinks — keeping the symlinked path lets CLI upgrades land
	// without rewriting every generated config.
	exe, err := os.Executable()
	if err != nil {
		logger.Warnf("Could not resolve the CLI's own path (%s); generated configs will look for `%s` on $PATH.", err, paths.CLIBinaryName)

		return ""
	}

	if !IsTransientPath(exe) {
		return exe
	}

	if policy == PreferPATH {
		logger.Infof("Running from a temporary path, so generated configs will call `%s` from $PATH instead of %s.", paths.CLIBinaryName, exe)
		warnIfNotOnPATH(logger)

		return ""
	}

	stable, err := CopyToStable(exe)
	if err != nil {
		logger.Warnf("The CLI is running from a temporary path (%s) and could not be copied to a stable one: %s", exe, err)
		logger.Warnf("Build tools calling back into the CLI will fail once that path is gone. Install the CLI and rerun this command.")

		return exe
	}

	logger.Infof("Running from a temporary path, so the CLI was copied to %s — that copy is what build tools will call.", stable)

	return stable
}

func warnIfNotOnPATH(logger log.Logger) {
	if _, err := exec.LookPath(paths.CLIBinaryName); err == nil {
		return
	}

	logger.Warnf("`%s` is not on your $PATH, so those calls will fail. Install the CLI, or rerun this command from an installed copy.", paths.CLIBinaryName)
}

// CopyToStable copies src to StablePath() with 0o755 perms, creating the parent
// directory if needed. Returns the destination path.
func CopyToStable(src string) (string, error) {
	dst, err := StablePath()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("create stable bin dir: %w", err)
	}

	in, err := os.Open(src) //nolint:gosec // src is the running CLI's own executable path
	if err != nil {
		return "", fmt.Errorf("open source binary %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755) //nolint:gosec // executable must be runnable
	if err != nil {
		return "", fmt.Errorf("open destination %s: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)

		return "", fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(dst)

		return "", fmt.Errorf("close destination %s: %w", dst, err)
	}

	return dst, nil
}
