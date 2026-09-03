// Package clibin decides how a generated config should name the CLI, given that
// other processes run it later: build tools call back into it (the Bazel
// credential helper, the Gradle token resolver), so what gets written has to
// still work after the command exits.
package clibin

import (
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

// Resolve returns how a generated config should name the CLI: "" when a bare
// `bitrise-build-cache` resolves on $PATH, which callers turn into that bare name,
// and the running executable's path otherwise.
//
// $PATH first, even when the running binary has a perfectly good path: a config
// full of absolute paths goes stale the moment an upgrade moves the binary, and
// both consumers here (Bazel's --credential_helper, the Gradle init script) accept
// a bare command name. An absolute path is the fallback for a host where nothing
// answers to that name, and "" also comes back when neither is available — the
// caller decides what that means for its config.
func Resolve(logger log.Logger) string {
	onPATH := OnPATH()

	// Do NOT EvalSymlinks — keeping the symlinked path lets CLI upgrades land
	// without rewriting every generated config.
	exe, err := os.Executable()
	if err != nil {
		logger.Warnf("Could not resolve the CLI's own path (%s).", err)
		if !onPATH {
			warnNotOnPATH(logger)
		}

		return ""
	}

	path := resolvePath(exe, onPATH)
	logResolution(logger, exe, path, onPATH)

	return path
}

// resolvePath is the decision itself, kept separate from the logging and the
// process state so every combination is testable.
func resolvePath(exe string, onPATH bool) string {
	switch {
	case onPATH:
		return ""
	case IsTransientPath(exe):
		// Nothing usable: a temporary path is gone by the time a build tool calls it.
		return ""
	}

	return exe
}

func logResolution(logger log.Logger, exe, path string, onPATH bool) {
	switch {
	case path != "":
		logger.Infof("`%s` is not on your $PATH, so generated configs will call %s directly.", paths.CLIBinaryName, path)
	case !onPATH:
		logger.Infof("Running from a temporary path (%s).", exe)
		warnNotOnPATH(logger)
	case !sameBinary(exe):
		// Worth saying out loud: the config will call a different build of the CLI
		// than the one writing it, which is the normal case for a dev build.
		onPath, _ := exec.LookPath(paths.CLIBinaryName)
		logger.Infof("Generated configs will call `%s` from $PATH (%s), not the running binary (%s).", paths.CLIBinaryName, onPath, exe)
	default:
		logger.Debugf("Generated configs will call `%s` from $PATH.", paths.CLIBinaryName)
	}
}

// sameBinary reports whether $PATH resolves to the running executable.
func sameBinary(exe string) bool {
	onPath, err := exec.LookPath(paths.CLIBinaryName)
	if err != nil {
		return false
	}

	return realPath(onPath) == realPath(exe)
}

func realPath(p string) string {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}

	return resolved
}

// OnPATH reports whether a bare `bitrise-build-cache` resolves to anything, so a
// caller can tell a usable fallback from one that would never spawn.
func OnPATH() bool {
	_, err := exec.LookPath(paths.CLIBinaryName)

	return err == nil
}

func warnNotOnPATH(logger log.Logger) {
	logger.Warnf("`%s` is not on your $PATH either, so those calls will fail. Install the CLI, or rerun this command from an installed copy.", paths.CLIBinaryName)
}
