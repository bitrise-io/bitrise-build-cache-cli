package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"path/filepath"

	"strings"

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
	AddXcelerateToPath      = "ℹ️ To start building, run `export PATH=~/.bitrise-xcelerate/bin:$PATH` or restart your terminal."
	startedProxy            = "Started xcelerate_proxy pid = %d"

	ErrFmtCreateXcodeConfig  = "failed to create Xcode config: %w"
	errFmtExecutable         = "executable: %w"
	errFmtFailedToStartProxy = "failed to start proxy: %w"
	errFmtFailedToCreatePID  = "failed to create pid file: %w"
)

//go:generate moq -out mocks/config_mock.go -pkg mocks . XcelerateConfig
type XcelerateConfig interface {
	Save(os utils.OsProxy, encoderFactory utils.EncoderFactory) error
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

		xparams := xcelerate.Params{
			BuildCacheEnabled: true,
			DebugLogging:      isDebugLogMode,
		}

		config := xcelerate.NewConfig(xparams, os.Getenv)

		return ActivateXcodeCommandFn(
			logger,
			utils.DefaultOsProxy{},
			utils.DefaultEncoderFactory{},
			config,
			func(path string, command string) Command {
				return utils.CommandWrapper{Wrapped: exec.Command(path, command)}
			},
			func(pid int, signum syscall.Signal) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			},
			os.Getenv,
		)
	},
}

//nolint:gochecknoglobals
// var activateXcodeParams = DefaultActivateXcodeParams()

func init() {
	activateCmd.AddCommand(activateXcodeCmd)
}

type ActivateXcodeParams struct {
}

func DefaultActivateXcodeParams() ActivateXcodeParams {
	return ActivateXcodeParams{}
}

func ActivateXcodeCommandFn(
	logger log.Logger,
	osProxy utils.OsProxy,
	encoderFactory utils.EncoderFactory,
	xconfig XcelerateConfig,
	commandFunc func(path string, command string) Command,
	killFunc func(pid int, signum syscall.Signal),
	envProvider func(string) string,
) error {
	if err := xconfig.Save(osProxy, encoderFactory); err != nil {
		return fmt.Errorf(ErrFmtCreateXcodeConfig, err)
	}

	err := startProxy(
		logger,
		osProxy,
		commandFunc,
		killFunc,
	)
	if err != nil {
		return fmt.Errorf(errFmtFailedToStartProxy, err)
	}

	if err := AddXcelerateCommandToPath(logger, osProxy); err != nil {
		return fmt.Errorf("failed to add xcelerate command to PATH: %w", err)
	}

	logger.Debugf("Xcelerate command added to PATH in ~/.bashrc and ~/.zshrc")
	logger.TInfof(ActivateXcodeSuccessful)
	logger.TInfof(AddXcelerateToPath)

	return nil
}

// nolint: godox
// TODO move to utils package
func AddXcelerateCommandToPath(logger log.Logger,
	osProxy utils.OsProxy) error {
	xceleratePath := xcelerate.PathFor(xcelerate.BinDir)

	homeDir, err := osProxy.UserHomeDir()
	if err != nil {
		return fmt.Errorf(xcelerate.ErrFmtDetermineHome, err)
	}

	pathContent := fmt.Sprintf("export PATH=%s:$PATH", xceleratePath)

	logger.Debugf("Adding xcelerate command to PATH in ~/.bashrc: %s", xceleratePath)
	err = AddContentOrCreateFile(logger,
		osProxy,
		filepath.Join(homeDir, ".bashrc"),
		"Bitrise Xcelerate",
		pathContent)
	if err != nil {
		return fmt.Errorf("failed to add xcelerate command to PATH: %w", err)
	}

	logger.Debugf("Adding xcelerate command to PATH in ~/.zshrc: %s", xceleratePath)
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
	commandFunc func(path string, command string) Command,
	killFunc func(pid int, signum syscall.Signal),
) error {
	exe, err := osProxy.Executable()
	if err != nil {
		return fmt.Errorf(errFmtExecutable, err)
	}

	cmd := commandFunc(exe, xcelerateProxyCmd.Use)

	// Detach into new process group so we can signal the whole group.
	cmd.SetSysProcAttr(&syscall.SysProcAttr{
		Setpgid: true, // create a new process group with pgid = pid
	})

	outf := xcelerate.PathFor(serverOut)
	errf := xcelerate.PathFor(serverErr)
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
	pidFilePth := xcelerate.PathFor(pidFile)
	if err := osProxy.WriteFile(pidFilePth, []byte(strconv.Itoa(pid)), 0644); err != nil {
		killFunc(pid, syscall.SIGKILL)

		return fmt.Errorf(errFmtFailedToCreatePID, err)
	}

	logger.TDonef(startedProxy, pid)

	return nil
}

//go:generate moq -out mocks/command_mock.go -pkg mocks . Command
type Command interface {
	Start() error
	SetStdout(file *os.File)
	SetStderr(file *os.File)
	SetStdin(file *os.File)
	SetSysProcAttr(sysProcAttr *syscall.SysProcAttr)
	PID() int
}
