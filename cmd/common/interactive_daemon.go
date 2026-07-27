package common

import (
	"context"
	"slices"

	"github.com/bitrise-io/go-utils/v2/log"

	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/permhint"
)

// daemonServicesForTools maps the wizard's tool selection to the background
// services those tools need. Gradle and Bazel talk to the cache directly.
func daemonServicesForTools(tools []string) []daemonpkg.Service {
	return daemonpkg.ServicesForTools(
		slices.Contains(tools, string(toolXcode)),
		slices.Contains(tools, string(toolCcache)),
	)
}

// startDaemonForTools registers + starts the proxies the selected tools need.
// Activation already succeeded by this point, so failures are reported with the
// manual fallback rather than failing the wizard.
func startDaemonForTools(ctx context.Context, logger log.Logger, tools []string) {
	services := daemonServicesForTools(tools)
	if len(services) == 0 {
		return
	}

	logger.Println()
	logger.TInfof("Starting the background cache services...")

	result, dPaths, err := daemonpkg.Bootstrap(ctx, logger, services)
	if err != nil {
		permhint.PrintIfApplicable(logger, err)
		logger.Warnf("Could not start the background services: %s", err)
		logger.Infof("Your build tools are activated. Start the services later with: %s daemon install", paths.CLIBinaryName)

		return
	}

	for _, st := range result.Statuses {
		logger.Donef("%s — running (%s)", st.Service.Name, result.BackendName)
	}

	switch result.BackendName {
	case "launchd":
		logger.Infof("Supervisor log dir: %s", dPaths.DaemonLogDir())
	case "systemd":
		logger.Infof("Supervisor log stream: journalctl --user -u %s", services[0].UnitName())
	}
	logger.Infof("Socket paths (for IDE configuration): %s daemon info", paths.CLIBinaryName)
}
