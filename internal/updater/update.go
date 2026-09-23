package updater

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

type Options struct {
	Executable string
	Logger     log.Logger
	DryRun     bool
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

		removeLegacySupervision(ctx, opts.Logger)
	case InstallUnknown:
		opts.Logger.Warnf("Could not classify the install method. Reinstall manually:")
		opts.Logger.Warnf("  curl --retry 5 -sSfL 'https://raw.githubusercontent.com/bitrise-io/bitrise-build-cache-cli/main/install/installer.sh' | sh -s -- -b <your-bindir>")
	}

	return nil
}

// An upgrade is the natural point to retire what an older CLI registered.
func removeLegacySupervision(ctx context.Context, logger log.Logger) {
	p, err := paths.Default()
	if err != nil {
		return
	}

	for _, svc := range []spawn.Service{spawn.XcelerateProxy(), spawn.CcacheHelper()} {
		if spawn.RemoveLegacySupervision(ctx, p, svc) {
			logger.Donef("Removed the %s service registration; the build now starts it on demand.", svc.Name)
		}
	}
}
