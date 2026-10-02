package xcelerate

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/clibin"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/envexport"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	ActivateXcodeSuccessful = "✅ Bitrise Build Cache for Xcode activated"
	AddXcelerateToPath      = "ℹ️ To start building, run `export PATH=~/.bitrise-xcelerate/bin:$PATH` or restart your terminal."

	ProxyLifecycleNotice = "ℹ️ The cache proxy starts automatically with your first `xcodebuild` and keeps serving later builds."
	ProxyRestartNotice   = "ℹ️ It does not survive a reboot or logout. Terminal builds restart it on their own; " +
		"for builds started from Xcode.app, run `bitrise-build-cache xcelerate start-proxy` first — " +
		"see docs/xcode-scheme-self-check.md for a scheme pre-action that does it for you."
	ErrFmtCreateXcodeConfig = "failed to create Xcode config: %w"

	xcodebuildWrapperScriptContent = `#!/bin/bash
set -e

if [ "${1-}" = "-version" ]; then
  %s "$@"
else
  %s/` + paths.XcelerateCLIBinaryName + ` xcelerate xcodebuild "$@"
fi
`
	xcrunWrapperScriptContent = `#!/bin/bash
set -e

if [ "${1-}" = "xcodebuild" ] && [ "${2-}" = "-version" ]; then
  %s "$@"
elif [ "${1-}" = "xcodebuild" ]; then
  shift
  %s/` + paths.XcelerateCLIBinaryName + ` xcelerate xcodebuild "$@"
else
  %s "$@"
fi
`
)

// Activate creates the Xcode build cache configuration, copies the CLI binary,
// and sets up the xcodebuild wrapper script.
func Activate(
	ctx context.Context,
	logger log.Logger,
	osProxy utils.OsProxy,
	commandFunc utils.CommandFunc,
	encoderFactory utils.EncoderFactory,
	decoderFactory utils.DecoderFactory,
	activateXcodeParams Params,
	envs map[string]string,
) error {
	overrideActivateXcodeParamsFromExistingConfig(
		logger, osProxy, &activateXcodeParams, decoderFactory, envs)

	// Resolve, not ResolveNoRefresh: a Build Hub runner carries no auth env vars at
	// all, and brokering its VM token is the only way to a credential there.
	// ResolveNoRefresh never brokers, so it failed the whole activation.
	authConfig, _, err := live.Default(logger).Resolve(ctx, envs)
	if err != nil {
		return fmt.Errorf("resolve auth config: %w", err)
	}

	benchmarkClient := configcommon.NewBenchmarkPhaseClient(consts.BitriseWebsiteBaseURL, authConfig, logger)

	config, err := NewConfig(
		ctx,
		logger,
		activateXcodeParams,
		envs,
		osProxy,
		commandFunc,
		envexport.New(envs, logger),
		benchmarkClient,
	)
	if err != nil {
		return fmt.Errorf("failed to create xcelerate config: %w", err)
	}

	if err := config.Save(logger, osProxy, encoderFactory); err != nil {
		return fmt.Errorf(ErrFmtCreateXcodeConfig, err)
	}

	ensureLogDir(logger, osProxy)

	// Materialise an env- or JWT-sourced credential: the proxy and the analytics
	// readers start in shells that never saw those variables.
	if _, _, err := live.Default(logger).ResolvePinned(ctx, envs, configcommon.IsCI(envs, osProxy)); err != nil {
		return fmt.Errorf("persist auth credentials: %w", err)
	}

	// Read-modify-write: Config.Save is a full overwrite, and a fresh Config here
	// would drop the credentials block that is the only credential store on a
	// keychain-less host.
	if err := multiplatformconfig.Update(osProxy, encoderFactory, decoderFactory, func(c *multiplatformconfig.Config) {
		c.DebugLogging = config.DebugLogging
	}); err != nil {
		return fmt.Errorf("failed to save multiplatform analytics config: %w", err)
	}
	logger.Infof("Wrote multiplatform analytics config: %s", multiplatformconfig.FilePath(osProxy))

	if _, _, err := clibin.InstallCLIAt(ctx, PathFor(osProxy, BinDir), clibin.InstallOpts{
		OsProxy:     osProxy,
		KillRunning: true,
		Basename:    paths.XcelerateCLIBinaryName,
	}, logger); err != nil {
		return fmt.Errorf("failed to copy xcelerate cli to ~/.bitrise-xcelerate/bin: %w", err)
	}

	if err := addXcelerateCommandToPathWithScriptWrapper(config, osProxy, logger, envs); err != nil { //nolint:contextcheck // envman export inside is fire-and-forget; ctx propagation would cascade through TemplateInventory/ApplyBenchmarkPhase for no operational gain
		return fmt.Errorf("failed to add xcelerate command: %w", err)
	}

	exportDerivedDataPath(logger, config, envs) //nolint:contextcheck // envman export inside is fire-and-forget, matching the wrapper-script export above

	logger.TInfof(ActivateXcodeSuccessful)
	logger.TInfof(AddXcelerateToPath)
	logger.TInfof(ProxyLifecycleNotice)
	logger.TInfof(ProxyRestartNotice)

	return nil
}

// ensureLogDir creates the dir the proxy would otherwise create on its first run,
// so the first build's health check doesn't report it as missing. Best-effort:
// the proxy still creates it, and a build must not fail over a log dir.
func ensureLogDir(logger log.Logger, osProxy utils.OsProxy) {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		logger.Debugf("Could not resolve the home dir for the xcelerate log dir: %s", err)

		return
	}

	if err := paths.EnsureDir(osProxy, paths.FromHome(home).XcelerateLogDir()); err != nil {
		logger.Debugf("Could not create the xcelerate log dir: %s", err)
	}
}

// exportDerivedDataPath publishes where the wrapper relocates DerivedData to, so cache steps can
// target the SPM checkouts under it.
func exportDerivedDataPath(logger log.Logger, config Config, envs map[string]string) {
	if !config.BuildCacheEnabled || config.BuildCacheSkipFlags || config.DisablePrefixMapping {
		return
	}

	p, err := paths.Default()
	if err != nil {
		logger.Debugf("Skipping %s export: %v", EnvDerivedDataPath, err)

		return
	}

	envexport.New(envs, logger).Export(EnvDerivedDataPath, p.XcodeManagedDerivedDataRoot())
}

// ---------------------------------------------------------------------------
// Private — activation helpers
// ---------------------------------------------------------------------------

func overrideActivateXcodeParamsFromExistingConfig(
	logger log.Logger,
	osProxy utils.OsProxy,
	activateXcodeParams *Params,
	decoderFactory utils.DecoderFactory,
	envs map[string]string,
) {
	if existingConfig, err := ReadConfig(osProxy, decoderFactory, envs); err == nil {
		if strings.Contains(existingConfig.OriginalXcodebuildPath, PathFor(osProxy, BinDir)) {
			logger.Warnf("Removing xcelerate wrapper as original xcodebuild path...")
			existingConfig.OriginalXcodebuildPath = ""
		}

		activateXcodeParams.XcodePathOverride = cmp.Or(
			activateXcodeParams.XcodePathOverride,
			existingConfig.OriginalXcodebuildPath,
		)

		if strings.Contains(existingConfig.OriginalXcrunPath, PathFor(osProxy, BinDir)) {
			logger.Warnf("Removing xcelerate wrapper as original xcrun path...")
			existingConfig.OriginalXcrunPath = ""
		}

		activateXcodeParams.XcrunPathOverride = cmp.Or(
			activateXcodeParams.XcrunPathOverride,
			existingConfig.OriginalXcrunPath,
		)
	} else if isXcelerateInPath(osProxy, envs) {
		logger.Warnf("It seems that the xcelerate config file is missing, but xcelerate is already in the PATH. \n" +
			"This will lead to unexpected behavior when determining the xcodebuild path. \n" +
			"Defaulting to /usr/bin/xcodebuild...")
		activateXcodeParams.XcodePathOverride = "/usr/bin/xcodebuild"
	}
}

func isXcelerateInPath(osProxy utils.OsProxy, envs map[string]string) bool {
	path := envs["PATH"]
	for _, p := range strings.Split(path, ":") {
		if strings.Contains(p, PathFor(osProxy, BinDir)) {
			return true
		}
	}

	return false
}

func addXcelerateCommandToPathWithScriptWrapper(
	config Config,
	osProxy utils.OsProxy,
	logger log.Logger,
	envs map[string]string,
) error {
	binPath := PathFor(osProxy, BinDir)
	if err := osProxy.MkdirAll(binPath, 0o755); err != nil {
		return fmt.Errorf("failed to create bin dir: %w", err)
	}

	scriptPath := filepath.Join(binPath, "xcodebuild")

	if err := osProxy.WriteFile(scriptPath,
		[]byte(fmt.Sprintf(xcodebuildWrapperScriptContent,
			config.OriginalXcodebuildPath,
			binPath)), 0o755); err != nil {
		return fmt.Errorf("failed to create xcodebuild wrapper script: %w", err)
	}
	logger.Infof("Wrote xcodebuild wrapper script: %s", scriptPath)

	scriptPath = filepath.Join(binPath, "xcrun")

	if err := osProxy.WriteFile(scriptPath,
		[]byte(fmt.Sprintf(xcrunWrapperScriptContent,
			config.OriginalXcrunPath,
			binPath,
			config.OriginalXcrunPath)), 0o755); err != nil {
		return fmt.Errorf("failed to create xcrun wrapper script: %w", err)
	}
	logger.Infof("Wrote xcrun wrapper script: %s", scriptPath)

	path := strings.ReplaceAll(envs["PATH"], binPath+":", "")
	path = strings.Join([]string{binPath, path}, ":")

	exporter := envexport.New(envs, logger)
	exporter.Export("PATH", path)
	exporter.ExportToShellRC(XcelerateShellRCBlockName, fmt.Sprintf("export PATH=%s:$PATH", binPath))

	return nil
}
