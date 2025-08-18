package utils

import (
	"os"
	"os/exec"
	"syscall"
)

type CommandWrapper struct {
	Wrapped *exec.Cmd
}

func (cmd CommandWrapper) SetStdout(file *os.File) {
	cmd.Wrapped.Stdout = file
}

func (cmd CommandWrapper) SetStderr(file *os.File) {
	cmd.Wrapped.Stderr = file
}

func (cmd CommandWrapper) SetStdin(file *os.File) {
	cmd.Wrapped.Stdin = file
}

func (cmd CommandWrapper) SetSysProcAttr(sysProcAttr *syscall.SysProcAttr) {
	cmd.Wrapped.SysProcAttr = sysProcAttr
}

func (cmd CommandWrapper) PID() int {
	if cmd.Wrapped.Process == nil {
		return 0
	}

	return cmd.Wrapped.Process.Pid
}

func (cmd CommandWrapper) Start() error {
	return cmd.Wrapped.Start() //nolint:wrapcheck
}
