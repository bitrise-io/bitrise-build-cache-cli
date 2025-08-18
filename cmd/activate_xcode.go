package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

const (
	pidFile   = "proxy.pid"
	serverOut = "proxy.out"
	serverErr = "proxy.err"

	activateXcode           = "Activate Bitrise Build Cache for Xcode"
	ActivateXcodeSuccessful = "✅ Bitrise Build Cache for Xcode activated"
	startedProxy            = "Started xcelerate_proxy pid = %d"

	ErrFmtCreateXcodeConfig  = "failed to create Xcode config: %w"
	errFmtDetermineHome      = "could not determine home: %w"
	errFmtExecutable         = "executable: %w"
	errFmtFailedToStartProxy = "failed to start proxy: %w"
	errFmtFailedToCreatePID  = "failed to create pid file: %w"
)

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

		return ActivateXcodeCommandFn(
			logger,
			utils.DefaultOsProxy{},
			utils.DefaultEncoderFactory{},
			&xcelerate.DefaultConfig{
				ProxyVersion:           "1.0.0",
				WrapperVersion:         "1.0.0",
				OriginalXcodebuildPath: "/usr/bin/xcodebuild",
				BuildCacheEnabled:      true,
			},
			func(path string, command string) Command {
				return CommandWrapper{wrapped: exec.Command(path, command)}
			},
			func(pid int, signum syscall.Signal) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			},
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
	config xcelerate.Config,
	commandFunc func(path string, command string) Command,
	killFunc func(pid int, signum syscall.Signal),
) error {
	if err := config.Save(osProxy, encoderFactory); err != nil {
		return fmt.Errorf(ErrFmtCreateXcodeConfig, err)
	}

	logger.TInfof(ActivateXcodeSuccessful)

	return startProxy(
		logger,
		osProxy,
		commandFunc,
		killFunc,
	)
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

	outf := xcelerate.XceleratePathFor(serverOut)
	errf := xcelerate.XceleratePathFor(serverErr)
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
	pidFilePth := xcelerate.XceleratePathFor(pidFile)
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

type CommandWrapper struct {
	wrapped *exec.Cmd
}

func (cmd CommandWrapper) SetStdout(file *os.File) {
	cmd.wrapped.Stdout = file
}

func (cmd CommandWrapper) SetStderr(file *os.File) {
	cmd.wrapped.Stderr = file
}

func (cmd CommandWrapper) SetStdin(file *os.File) {
	cmd.wrapped.Stdin = file
}

func (cmd CommandWrapper) SetSysProcAttr(sysProcAttr *syscall.SysProcAttr) {
	cmd.wrapped.SysProcAttr = sysProcAttr
}

func (cmd CommandWrapper) PID() int {
	if cmd.wrapped.Process == nil {
		return 0
	}

	return cmd.wrapped.Process.Pid
}

func (cmd CommandWrapper) Start() error {
	return cmd.wrapped.Start() //nolint:wrapcheck
}
