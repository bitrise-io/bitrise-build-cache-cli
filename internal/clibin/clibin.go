// Package clibin decides how a generated config should name the CLI, given that
// other processes run it later: build tools call back into it (the Bazel
// credential helper, the Gradle token resolver) and supervisors pin it into their
// configs, so what gets written has to still work after the command exits.
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

// Resolve returns the CLI path to embed in a generated config: the running
// executable, or "" when that executable sits somewhere transient — a path from
// `go run` is gone by the time a build tool calls it. Callers turn "" into the
// bare binary name, which both Bazel and Gradle resolve through $PATH at call
// time, and which also survives a CLI upgrade that moves the binary.
func Resolve(logger log.Logger) string {
	// Do NOT EvalSymlinks — keeping the symlinked path lets CLI upgrades land
	// without rewriting every generated config.
	exe, err := os.Executable()
	if err != nil {
		logger.Warnf("Could not resolve the CLI's own path (%s); generated configs will look for `%s` on $PATH.", err, paths.CLIBinaryName)
		warnIfNotOnPATH(logger)

		return ""
	}

	if !IsTransientPath(exe) {
		return exe
	}

	logger.Infof("Running from a temporary path, so generated configs will call `%s` from $PATH instead of %s.", paths.CLIBinaryName, exe)
	warnIfNotOnPATH(logger)

	return ""
}

// OnPATH reports whether a bare `bitrise-build-cache` resolves to anything, so a
// caller can tell a usable fallback from one that would never spawn.
func OnPATH() bool {
	_, err := exec.LookPath(paths.CLIBinaryName)

	return err == nil
}

func warnIfNotOnPATH(logger log.Logger) {
	if OnPATH() {
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
