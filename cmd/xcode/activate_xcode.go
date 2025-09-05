package xcode

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/spf13/cobra"

	clicmd "github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	pidFile   = "proxy.pid"
	serverOut = "proxy.out.log"
	serverErr = "proxy.err.log"

	activateXcode           = "Activate Bitrise Build Cache for Xcode"
	ActivateXcodeSuccessful = "✅ Bitrise Build Cache for Xcode activated"
	AddXcelerateToPath      = "ℹ️ To start building, run `export PATH=~/.bitrise-xcelerate/bin:$PATH` or restart your terminal."
	startedProxy            = "Started xcelerate_proxy pid = %d"

	ErrFmtCreateXcodeConfig = "failed to create Xcode config: %w"

	cliBasename          = "bitrise-build-cache-cli"
	wrapperScriptContent = `#!/bin/bash
set -euxo pipefail
%s/bitrise-build-cache-cli xcelerate xcodebuild "$@"
`
)

//go:generate moq -stub -out mocks/config_mock.go -pkg mocks . XcelerateConfig
type XcelerateConfig interface {
	Save(logger log.Logger, os utils.OsProxy, encoderFactory utils.EncoderFactory) error
}

// activateXcodeCmd represents the `xcode` subcommand under `activate`
var activateXcodeCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "xcode",
	Short: "Activate Bitrise Build Cache for Xcode",
	Long: `Activate Bitrise Build Cache for Xcode.
This command will:

- Create a config file at ~/.bitrise-xcelerate/config.json with the Xcode proxy and wrapper versions.
- Download an executable proxy to enable xcode compilation cache connecting to the Bitrise Build Cache.
- Create an executable wrapper for xcodebuild that will use the proxy to connect to the Bitrise Build Cache.
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(clicmd.IsDebugLogMode)
		logger.TInfof(activateXcode)
		logger.Infof("Activate Xcode params: %+v", activateXcodeParams)

		activateXcodeParams.DebugLogging = clicmd.IsDebugLogMode

		return ActivateXcodeCommandFn(
			cmd.Context(),
			logger,
			utils.DefaultOsProxy{},
			utils.DefaultCommandFunc(),
			utils.DefaultEncoderFactory{},
			utils.DefaultDecoderFactory{},
			activateXcodeParams,
			utils.AllEnvs(),
		)
	},
}

//nolint:gochecknoglobals
var activateXcodeParams = xcelerate.DefaultParams()

func init() {
	clicmd.ActivateCmd.AddCommand(activateXcodeCmd)
	activateXcodeCmd.Flags().StringVar(
		&activateXcodeParams.ProxySocketPathOverride,
		"proxy-socket-path",
		activateXcodeParams.ProxySocketPathOverride,
		"Override the proxy socket path. This is useful for testing purposes.",
	)
	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.BuildCacheEnabled,
		"cache",
		activateXcodeParams.BuildCacheEnabled,
		"Activate xcode compilation cache.")
	activateXcodeCmd.Flags().StringVar(&activateXcodeParams.XcodePathOverride,
		"xcode-path",
		activateXcodeParams.XcodePathOverride,
		`Override the xcodebuild path. By default it will use the $(which xcodebuild) command to determine the path, and if that fails, it will use the default path: /usr/bin/xcodebuild.

Useful if there are multiple Xcode versions installed and you want to use a specific one.`,
	)
}

func ActivateXcodeCommandFn(
	ctx context.Context,
	logger log.Logger,
	osProxy utils.OsProxy,
	commandFunc utils.CommandFunc,
	encoderFactory utils.EncoderFactory,
	decoderFactory utils.DecoderFactory,
	activateXcodeParams xcelerate.Params,
	envs map[string]string,
) error {
	// if there was an existing config, use its xcodebuild path if not overridden by flag
	if existingConfig, err := xcelerate.ReadConfig(osProxy, decoderFactory); err == nil {
		activateXcodeParams.XcodePathOverride = cmp.Or(
			activateXcodeParams.XcodePathOverride,
			existingConfig.OriginalXcodebuildPath,
		)
	}

	config, err := xcelerate.NewConfig(
		ctx,
		logger,
		activateXcodeParams,
		envs,
		osProxy,
		commandFunc,
	)
	if err != nil {
		return fmt.Errorf("failed to create xcelerate config: %w", err)
	}

	if err := config.Save(logger, osProxy, encoderFactory); err != nil {
		return fmt.Errorf(ErrFmtCreateXcodeConfig, err)
	}

	// copy cli to ~/.bitrise-xcelerate/bin/bitrise-build-cache-cli
	if err := copyCLIToXcelerateBinDir(ctx, osProxy, logger); err != nil {
		return fmt.Errorf("failed to copy xcelerate cli to ~/.bitrise-xcelerate/bin: %w", err)
	}

	if err := addXcelerateCommandToPathWithScriptWrapper(ctx, osProxy, commandFunc, logger, envs); err != nil {
		return fmt.Errorf("failed to add xcelerate command: %w", err)
	}

	logger.Debugf("Xcelerate command added to ~/.bashrc and ~/.zshrc")
	logger.TInfof(ActivateXcodeSuccessful)
	logger.TInfof(AddXcelerateToPath)

	return nil
}

func copyCLIToXcelerateBinDir(context context.Context, osProxy utils.OsProxy, logger log.Logger) error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}

	reader, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source executable: %w", err)
	}
	defer reader.Close()

	binPath := xcelerate.PathFor(osProxy, xcelerate.BinDir)
	if err := osProxy.MkdirAll(binPath, 0o755); err != nil {
		return fmt.Errorf("failed to create bin dir: %w", err)
	}

	target := filepath.Join(binPath, cliBasename)

	if err := makeSureCLIIsNotRunning(context, target, logger); err != nil {
		return fmt.Errorf("failed to ensure cli is not running: %w", err)
	}

	writer, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create destination executable: %w", err)
	}
	defer writer.Close()

	if _, err = io.Copy(writer, reader); err != nil {
		return fmt.Errorf("failed to copy executable: %w", err)
	}

	logger.TInfof("Copied CLI to %s", target)

	return nil
}

// makeSureCLIIsNotRunning checks if there is any running CLI and tries to terminate/kill it.
func makeSureCLIIsNotRunning(ctx context.Context, target string, logger log.Logger) error {
	processes, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to list processes: %w", err)
	}

	for _, p := range processes {
		exe, err := p.ExeWithContext(ctx)
		if err != nil {
			continue
		}
		if exe != target {
			continue
		}

		logger.TWarnf("Terminating already running CLI (pid: %d)", p.Pid)
		if err := p.TerminateWithContext(ctx); err != nil {
			logger.TWarnf("Failed to terminate already running CLI, attempting to kill it")

			if err := p.KillWithContext(ctx); err != nil {
				return fmt.Errorf("failed to kill already running CLI (pid: %d): %w", p.Pid, err)
			}
		}
	}

	return nil
}

// addXcelerateCommandToPathWithScriptWrapper creates a script that wraps the CLI and adds it to the PATH
func addXcelerateCommandToPathWithScriptWrapper(
	ctx context.Context,
	osProxy utils.OsProxy,
	commandFunc utils.CommandFunc,
	logger log.Logger,
	envs map[string]string,
) error {
	homeDir, err := osProxy.UserHomeDir()
	if err != nil {
		return fmt.Errorf(xcelerate.ErrFmtDetermineHome, err)
	}

	binPath := xcelerate.PathFor(osProxy, xcelerate.BinDir)
	if err := osProxy.MkdirAll(binPath, 0o755); err != nil {
		return fmt.Errorf("failed to create bin dir: %w", err)
	}

	// create a script that wraps the CLI to preserve any arguments and environment variables
	scriptPath := filepath.Join(binPath, "xcodebuild")
	logger.Debugf("Creating xcodebuild wrapper script: %s", scriptPath)
	if err := osProxy.WriteFile(scriptPath, []byte(fmt.Sprintf(wrapperScriptContent, binPath)), 0o755); err != nil {
		return fmt.Errorf("failed to create xcodebuild wrapper script: %w", err)
	}

	pathContent := fmt.Sprintf("export PATH=%s:$PATH", binPath)

	addPathToEnvman(ctx, commandFunc, binPath, envs, logger)

	logger.Debugf("Adding xcelerate command to PATH in ~/.bashrc: %s", binPath)
	err = utils.AddContentOrCreateFile(logger,
		osProxy,
		filepath.Join(homeDir, ".bashrc"),
		"Bitrise Xcelerate",
		pathContent)
	if err != nil {
		return fmt.Errorf("failed to add xcelerate command to PATH in bashrc: %w", err)
	}

	logger.Debugf("Adding xcelerate command to PATH in ~/.zshrc: %s", binPath)
	err = utils.AddContentOrCreateFile(logger,
		osProxy,
		filepath.Join(homeDir, ".zshrc"),
		"# Bitrise Xcelerate",
		pathContent)
	if err != nil {
		return fmt.Errorf("failed to add xcelerate command to PATH in zshrc: %w", err)
	}

	return nil
}

func addPathToEnvman(
	ctx context.Context,
	commandFunc utils.CommandFunc,
	binPath string,
	envs map[string]string,
	logger log.Logger,
) {
	// remove any existing entry
	path := strings.ReplaceAll(envs["PATH"], binPath+":", "")
	// prepend our bin path
	path = strings.Join([]string{binPath, path}, ":")

	command := commandFunc(
		ctx,
		"envman",
		"add",
		"--key",
		"PATH",
		"--value",
		path,
	)
	if output, err := command.CombinedOutput(); err != nil {
		logger.Debugf("Failed to start envman command: %s", string(output))

		return
	}

	logger.TInfof("Added xcelerate command to envman PATH: %s", path)
}
