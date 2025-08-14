package xcodeargs

import (
	"os"
	"os/exec"
)

//go:generate moq -out mocks/runner_mock.go -pkg mocks . Runner
type Runner interface {
	Run(args []string) error
}

type DefaultRunner struct{}

func (runner *DefaultRunner) Run(args []string) error {
	innerCmd := exec.Command("xcodebuild", args...)
	innerCmd.Stdout = os.Stdout
	innerCmd.Stderr = os.Stderr
	innerCmd.Stdin = os.Stdin

	// Intentionally returning xcode error unwrapped

	//nolint:wrapcheck
	return innerCmd.Run()
}
