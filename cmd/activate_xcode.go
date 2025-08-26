package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"

	"path/filepath"

	"strings"

	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/stringmerge"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

const (
	pidFile   = "proxy.pid"
	serverOut = "proxy.out.log"
	serverErr = "proxy.err.log"

	activateXcode           = "Activate Bitrise Build Cache for Xcode"
	ActivateXcodeSuccessful = "✅ Bitrise Build Cache for Xcode activated"
	AddXcelerateToPath      = "ℹ️ To start building, run `alias xcodebuild='~/.bitrise-xcelerate/bin/bitrise-build-cache-cli xcelerate xcodebuild'` or restart your terminal."
	startedProxy            = "Started xcelerate_proxy pid = %d"

	ErrFmtCreateXcodeConfig  = "failed to create Xcode config: %w"
	errFmtExecutable         = "executable: %w"
	errFmtFailedToStartProxy = "failed to start proxy: %w"
	errFmtFailedToCreatePID  = "failed to create pid file: %w"
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

		// copy cli into ~/.bitrise-xcelerate/bin
		cliPath, err := copyCLIToXcelerateBinDir(osProxy)
		if err != nil {
			return fmt.Errorf("failed to copy xcelerate cli to ~/.bitrise-xcelerate/bin: %w", err)
		}

		return ActivateXcodeCommandFn(
			cliPath,
			logger,
			osProxy,
			utils.DefaultEncoderFactory{},
			config,
			utils.DefaultCommandFunc(),
			func(pid int, signum syscall.Signal) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			},
		)
	},
}

func copyCLIToXcelerateBinDir(osProxy utils.OsProxy) (string, error) {
	src, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to determine executable path: %w", err)
	}

	reader, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("failed to open source executable: %w", err)
	}
	defer reader.Close()

	binDir := xcelerate.PathFor(osProxy, "bin")
	if err := osProxy.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin dir: %w", err)
	}

	basename := filepath.Base(src)
	target := filepath.Join(binDir, basename)
	writer, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create destination executable: %w", err)
	}
	defer writer.Close()

	if _, err = io.Copy(writer, reader); err != nil {
		return "", fmt.Errorf("failed to copy executable: %w", err)
	}

	return target, nil
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
	cliPath string,
	logger log.Logger,
	osProxy utils.OsProxy,
	encoderFactory utils.EncoderFactory,
	xconfig XcelerateConfig,
	commandFunc utils.CommandFunc,
	killFunc func(pid int, signum syscall.Signal),
) error {
	if err := xconfig.Save(logger, osProxy, encoderFactory); err != nil {
		return fmt.Errorf(ErrFmtCreateXcodeConfig, err)
	}

	if activateXcodeParams.BuildCacheEnabled {
		logger.TInfof("Cache enabled, starting xcelerate proxy...")

		err := startProxy(
			logger,
			osProxy,
			commandFunc,
			killFunc,
		)
		if err != nil {
			return fmt.Errorf(errFmtFailedToStartProxy, err)
		}
	}

	if err := AddXcelerateCommandAlias(cliPath, logger, osProxy); err != nil {
		return fmt.Errorf("failed to add xcelerate command: %w", err)
	}

	logger.Debugf("Xcelerate command added to ~/.bashrc and ~/.zshrc")
	logger.TInfof(ActivateXcodeSuccessful)
	logger.TInfof(AddXcelerateToPath)

	return nil
}

// nolint: godox
// TODO move to utils package
func AddXcelerateCommandAlias(cliPath string, logger log.Logger, osProxy utils.OsProxy) error {
	homeDir, err := osProxy.UserHomeDir()
	if err != nil {
		return fmt.Errorf(xcelerate.ErrFmtDetermineHome, err)
	}

	pathContent := fmt.Sprintf("alias xcodebuild='%s xcelerate xcodebuild'", cliPath)

	logger.Debugf("Adding xcelerate command as alias to ~/.bashrc: %s", pathContent)
	err = AddContentOrCreateFile(logger,
		osProxy,
		filepath.Join(homeDir, ".bashrc"),
		"Bitrise Xcelerate",
		pathContent)
	if err != nil {
		return fmt.Errorf("failed to add xcelerate command to PATH: %w", err)
	}

	logger.Debugf("Adding xcelerate command as alias to ~/.zshrc: %s", pathContent)
	err = AddContentOrCreateFile(logger,
		osProxy,
		filepath.Join(homeDir, ".zshrc"),
		"# Bitrise Xcelerate",
		pathContent)

	return err
}

// nolint: godox
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

	err = osProxy.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	logger.Debugf("Updated file %s with content in block %s", filePath, blockSuffix)

	return nil
}

func startProxy(
	logger log.Logger,
	osProxy utils.OsProxy,
	commandFunc utils.CommandFunc,
	killFunc func(pid int, signum syscall.Signal),
) error {
	exe, err := osProxy.Executable()
	if err != nil {
		return fmt.Errorf(errFmtExecutable, err)
	}

	cmd := commandFunc(context.Background(), exe, "xcelerate", xcelerateProxyCmd.Use)

	// Detach into new process group so we can signal the whole group.
	cmd.SetSysProcAttr(&syscall.SysProcAttr{
		Setpgid: true, // create a new process group with pgid = pid
	})

	outf := xcelerate.PathFor(osProxy, serverOut)
	errf := xcelerate.PathFor(osProxy, serverErr)
	outFile, err := osProxy.OpenFile(outf, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	defer outFile.Close()

	errFile, err := osProxy.OpenFile(errf, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open error file: %w", err)
	}
	defer errFile.Close()

	cmd.SetStdout(outFile)
	cmd.SetStderr(errFile)
	cmd.SetStdin(nil)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf(errFmtFailedToStartProxy, err)
	}

	pid := cmd.PID()
	pidFilePth := xcelerate.PathFor(osProxy, pidFile)
	if err := osProxy.WriteFile(pidFilePth, []byte(strconv.Itoa(pid)), 0644); err != nil {
		killFunc(pid, syscall.SIGKILL)

		return fmt.Errorf(errFmtFailedToCreatePID, err)
	}

	logger.TDonef(startedProxy, pid)

	return nil
}
