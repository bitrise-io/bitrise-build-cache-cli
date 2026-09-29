package clibin

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// UserLocalBinDir returns $HOME/.local/bin, the canonical persistent, on-PATH
// (once the user opts in) install location shared by the CI and dev flows.
func UserLocalBinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	return filepath.Join(home, ".local", "bin"), nil
}

// EnsureInstalledInUserLocalBin copies the running CLI to
// $HOME/.local/bin/bitrise-build-cache unless a bare `bitrise-build-cache`
// already resolves on $PATH or the running binary is already at that target.
// Returns the target path and whether a copy happened.
//
// Skipping when OnPATH() is true keeps package managers (brew, apt) canonical;
// the user already has a usable install and we do not want to fight it.
func EnsureInstalledInUserLocalBin(logger log.Logger) (string, bool, error) {
	binDir, err := UserLocalBinDir()
	if err != nil {
		return "", false, err
	}
	target := filepath.Join(binDir, paths.CLIBinaryName)

	if OnPATH() {
		return target, false, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return "", false, fmt.Errorf("resolve executable: %w", err)
	}
	if realPath(exe) == realPath(target) {
		return target, false, nil
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", false, fmt.Errorf("create %s: %w", binDir, err)
	}
	if err := copyExecutable(exe, target); err != nil {
		return "", false, fmt.Errorf("copy %s -> %s: %w", exe, target, err)
	}

	logger.Infof("Installed `%s` to %s.", paths.CLIBinaryName, target)
	logger.Infof("Add %s to your PATH so build tools can resolve `%s` by name.", binDir, paths.CLIBinaryName)

	return target, true, nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err //nolint:wrapcheck
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err //nolint:wrapcheck
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)

		return err //nolint:wrapcheck
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)

		return err //nolint:wrapcheck
	}

	return os.Rename(tmp, dst) //nolint:wrapcheck
}
