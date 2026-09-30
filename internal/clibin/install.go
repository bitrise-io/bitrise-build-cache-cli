package clibin

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// LocalBinRelative is the $HOME-relative canonical persistent, on-PATH (once
// the user opts in) install location shared by the CI and dev flows.
const LocalBinRelative = ".local/bin"

// UserLocalBinDir returns $HOME/.local/bin.
func UserLocalBinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	return filepath.Join(home, LocalBinRelative), nil
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
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	return WriteExecutableAtomically(filepath.Dir(dst), dst, in)
}

// WriteExecutableAtomically renames a temp copy over target, so a failed write
// or a still-running old executable can't leave a corrupted binary in place.
// dir must be the directory that holds target (the temp file lives there so
// the rename stays on one filesystem).
func WriteExecutableAtomically(dir, target string, src io.Reader) error {
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp executable: %w", err)
	}
	defer func() {
		_ = os.Remove(tmp.Name())
	}()

	if _, err = io.Copy(tmp, src); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("copy executable: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp executable: %w", err)
	}

	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return fmt.Errorf("chmod temp executable: %w", err)
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("move executable into place: %w", err)
	}

	return nil
}
