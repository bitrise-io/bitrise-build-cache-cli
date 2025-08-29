package cmd

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

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/stringmerge"
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

//go:generate moq -out mocks/config_mock.go -pkg mocks . XcelerateConfig
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
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof(activateXcode)
		logger.Infof("Activate Xcode params: %+v", activateXcodeParams)

		activateXcodeParams.DebugLogging = isDebugLogMode

		// if there was an existing config, use its xcodebuild path if not overridden by flag
		if existingConfig, err := xcelerate.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{}); err == nil {
			activateXcodeParams.XcodePathOverride = cmp.Or(activateXcodeParams.XcodePathOverride, existingConfig.OriginalXcodebuildPath)
		}

		osProxy := utils.DefaultOsProxy{}
		config, err := xcelerate.NewConfig(
			cmd.Context(),
			logger,
			activateXcodeParams,
			utils.AllEnvs(),
			osProxy,
			utils.DefaultCommandFunc(),
		)
		if err != nil {
			return fmt.Errorf("failed to create xcelerate config: %w", err)
		}

		// copy cli to ~/.bitrise-xcelerate/bin/bitrise-build-cache-cli
		if err := copyCLIToXcelerateBinDir(cmd.Context(), osProxy, logger); err != nil {
			return fmt.Errorf("failed to copy xcelerate cli to ~/.bitrise-xcelerate/bin: %w", err)
		}

		return ActivateXcodeCommandFn(
			logger,
			osProxy,
			utils.DefaultEncoderFactory{},
			config,
		)
	},
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

//nolint:gochecknoglobals
var activateXcodeParams = xcelerate.DefaultParams()

func init() {
	activateCmd.AddCommand(activateXcodeCmd)
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
	logger log.Logger,
	osProxy utils.OsProxy,
	encoderFactory utils.EncoderFactory,
	xconfig XcelerateConfig,
) error {
	if err := xconfig.Save(logger, osProxy, encoderFactory); err != nil {
		return fmt.Errorf(ErrFmtCreateXcodeConfig, err)
	}

	if err := AddXcelerateCommandToPathWithScriptWrapper(osProxy, logger); err != nil {
		return fmt.Errorf("failed to add xcelerate command: %w", err)
	}

	logger.Debugf("Xcelerate command added to ~/.bashrc and ~/.zshrc")
	logger.TInfof(ActivateXcodeSuccessful)
	logger.TInfof(AddXcelerateToPath)

	return nil
}

// AddXcelerateCommandToPathWithScriptWrapper creates a script that wraps the CLI and adds it to the PATH
// TODO move to utils package
func AddXcelerateCommandToPathWithScriptWrapper(osProxy utils.OsProxy, logger log.Logger) error {
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

	logger.Debugf("Adding xcelerate command to PATH in ~/.bashrc: %s", binPath)
	err = AddContentOrCreateFile(logger,
		osProxy,
		filepath.Join(homeDir, ".bashrc"),
		"Bitrise Xcelerate",
		pathContent)
	if err != nil {
		return fmt.Errorf("failed to add xcelerate command to PATH: %w", err)
	}

	logger.Debugf("Adding xcelerate command to PATH in ~/.zshrc: %s", binPath)
	err = AddContentOrCreateFile(logger,
		osProxy,
		filepath.Join(homeDir, ".zshrc"),
		"# Bitrise Xcelerate",
		pathContent)

	return err
}

// TODO move to utils package
func AddContentOrCreateFile(
	logger log.Logger,
	osProxy utils.OsProxy,
	filePath string,
	blockSuffix string,
	content string,
) error {
	// Check if the file exists
	currentContent, exists, err := osProxy.ReadFileIfExists(filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	if !exists {
		currentContent = ""
		logger.Debugf("File %s does not exist, creating", filePath)
	}

	content = stringmerge.ChangeContentInBlock(
		currentContent,
		fmt.Sprintf("# [start] %s", strings.TrimSpace(blockSuffix)),
		fmt.Sprintf("# [end] %s", strings.TrimSpace(blockSuffix)),
		content,
	)

	err = osProxy.WriteFile(filePath, []byte(content), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	logger.Debugf("Updated file %s with content in block %s", filePath, blockSuffix)

	return nil
}
