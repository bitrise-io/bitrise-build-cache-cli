package clibin

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// LocalBinRelative is the $HOME-relative canonical persistent, on-PATH (once
// the user opts in) install location shared by the CI and dev flows.
const LocalBinRelative = ".local/bin"

// InstallOpts controls how InstallCLIAt places the CLI.
type InstallOpts struct {
	// OsProxy is used for MkdirAll; nil falls back to os.MkdirAll.
	OsProxy utils.OsProxy
	// KillRunning terminates any process whose executable path resolves to the
	// target before the atomic rename. Required for dirs that are already on
	// $PATH from a previous activation — otherwise the second activation would
	// try to rename over a busy binary.
	KillRunning bool
	// Basename overrides the on-disk filename; empty defaults to paths.CLIBinaryName.
	Basename string
}

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
func EnsureInstalledInUserLocalBin(ctx context.Context, logger log.Logger) (string, bool, error) {
	binDir, err := UserLocalBinDir()
	if err != nil {
		return "", false, err
	}

	if OnPATH() {
		return filepath.Join(binDir, paths.CLIBinaryName), false, nil
	}

	target, installed, err := InstallCLIAt(ctx, binDir, InstallOpts{}, logger)
	if err != nil {
		return "", false, err
	}

	if installed {
		logger.Infof("Add %s to your PATH so build tools can resolve `%s` by name.", binDir, paths.CLIBinaryName)
	}

	return target, installed, nil
}

// InstallCLIAt copies the running CLI to dir/<basename> using the atomic write
// helper. When opts.KillRunning is set, any process whose executable resolves
// to the target is terminated first, so the rename cannot fail on a busy file.
// Returns the target path, whether a copy happened, and any error.
func InstallCLIAt(ctx context.Context, dir string, opts InstallOpts, logger log.Logger) (string, bool, error) {
	basename := opts.Basename
	if basename == "" {
		basename = paths.CLIBinaryName
	}
	target := filepath.Join(dir, basename)

	exe, err := os.Executable()
	if err != nil {
		return "", false, fmt.Errorf("resolve executable: %w", err)
	}
	if realPath(exe) == realPath(target) {
		logger.Debugf("CLI already in place at %s", target)

		return target, false, nil
	}

	mkdirAll := os.MkdirAll
	if opts.OsProxy != nil {
		mkdirAll = opts.OsProxy.MkdirAll
	}
	if err := mkdirAll(dir, 0o755); err != nil {
		return "", false, fmt.Errorf("create %s: %w", dir, err)
	}

	if opts.KillRunning {
		if err := terminateProcessAtPath(ctx, target, logger); err != nil {
			return "", false, fmt.Errorf("terminate running CLI at %s: %w", target, err)
		}
	}

	if err := copyExecutable(exe, target); err != nil {
		return "", false, fmt.Errorf("copy %s -> %s: %w", exe, target, err)
	}

	logger.Infof("Installed `%s` to %s.", basename, target)

	return target, true, nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	return WriteExecutableAtomically(dst, in)
}

// terminateProcessAtPath ends any process whose executable path resolves to
// target, excluding the current process. Callers that write to a pinned dir
// already on $PATH need this — otherwise a re-activation would try to rename
// over the running copy of itself launched by a previous activation.
func terminateProcessAtPath(ctx context.Context, target string, logger log.Logger) error {
	processes, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return fmt.Errorf("list processes: %w", err)
	}

	for _, p := range processes {
		if int(p.Pid) == os.Getpid() {
			continue
		}

		exe, err := p.ExeWithContext(ctx)
		if err != nil {
			continue
		}

		if exe != target {
			continue
		}

		logger.Warnf("Terminating already running CLI (pid: %d)", p.Pid)

		if err := p.TerminateWithContext(ctx); err != nil {
			logger.Warnf("Failed to terminate already running CLI, attempting to kill it")

			if err := p.KillWithContext(ctx); err != nil {
				return fmt.Errorf("kill running CLI (pid: %d): %w", p.Pid, err)
			}
		}

		waitForProcessExit(ctx, p, logger)
	}

	return nil
}

func waitForProcessExit(ctx context.Context, p *process.Process, logger log.Logger) {
	for range 50 {
		if running, err := p.IsRunningWithContext(ctx); err == nil && !running {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}

	logger.Warnf("Already running CLI (pid: %d) did not exit in time", p.Pid)
}

// WriteExecutableAtomically renames a temp copy over target, so a failed write
// or a still-running old executable can't leave a corrupted binary in place.
func WriteExecutableAtomically(target string, src io.Reader) error {
	dir := filepath.Dir(target)
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
