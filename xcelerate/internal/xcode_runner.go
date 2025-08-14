package internal

import (
	"os"
	"os/exec"
)

//go:generate moq -out mocks/xcode_runner.go -pkg mocks . XcodeRunner
type XcodeRunner interface {
	Run(args []string) error
}

type DefaultXcodeRunner struct{}

func (runner *DefaultXcodeRunner) Run(args []string) error {
	innerCmd := exec.Command("xcodebuild", args...)
	innerCmd.Stdout = os.Stdout
	innerCmd.Stderr = os.Stderr
	innerCmd.Stdin = os.Stdin
	return innerCmd.Run()
}
