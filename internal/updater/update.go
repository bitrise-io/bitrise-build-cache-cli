package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
)

type Options struct {
	Executable    string
	Logger        log.Logger
	DryRun        bool
	RestartDaemon func(ctx context.Context) error
}

func Update(ctx context.Context, opts Options) error {
	method := DetectInstallMethod(opts.Executable)
	opts.Logger.Infof("Detected install method: %s (binary at %s)", method, opts.Executable)

	switch method {
	case InstallBrew:
		PrintBrewUpgrade(opts.Logger)
	case InstallManual:
		if _, err := ManualUpgrade(ctx, ManualOptions{
			Bindir: filepath.Dir(opts.Executable),
			Logger: opts.Logger,
			DryRun: opts.DryRun,
		}); err != nil {
			return fmt.Errorf("manual upgrade: %w", err)
		}

		if opts.DryRun {
			return nil
		}

		home, homeErr := os.UserHomeDir()
		if homeErr != nil || !DaemonInstalled(home) {
			return nil //nolint:nilerr // missing home dir => skip daemon restart, not a fatal upgrade error
		}

		opts.Logger.Infof("Restarting daemon to pick up the new binary")

		restart := opts.RestartDaemon
		if restart == nil {
			restart = defaultRestartDaemon
		}

		if err := restart(ctx); err != nil {
			opts.Logger.Warnf("Daemon restart failed: %v — run `bitrise-build-cache daemon restart` manually.", err)
		} else {
			opts.Logger.Donef("Daemon restarted")
		}
	case InstallUnknown:
		opts.Logger.Warnf("Could not classify the install method. Reinstall manually:")
		opts.Logger.Warnf("  curl --retry 5 -sSfL 'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' | sh -s -- -b <your-bindir>")
	}

	return nil
}

func defaultRestartDaemon(ctx context.Context) error {
	backend, err := daemonpkg.DefaultBackend()
	if err != nil {
		return err //nolint:wrapcheck // sentinel
	}

	paths, err := daemonpkg.NewPaths()
	if err != nil {
		return err //nolint:wrapcheck // already context-rich
	}

	if _, err := daemonpkg.Down(ctx, backend, paths, daemonpkg.DefaultServices()); err != nil {
		return err //nolint:wrapcheck // already context-rich
	}

	if _, err := daemonpkg.Up(ctx, backend, paths, daemonpkg.DefaultServices()); err != nil {
		return err //nolint:wrapcheck // already context-rich
	}

	return nil
}
