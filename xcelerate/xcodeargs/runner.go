package xcodeargs

import (
	"context"
	"os"
	"os/exec"
)

//go:generate moq -out mocks/context_mock.go -pkg mocks . Context
type Context = context.Context

//go:generate moq -out mocks/runner_mock.go -pkg mocks . Runner
type Runner interface {
	Run(ctx Context, args []string) error
}

type DefaultRunner struct{}

func (runner *DefaultRunner) Run(ctx Context, args []string) error {
	innerCmd := exec.CommandContext(ctx, "xcodebuild", args...)
	innerCmd.Stdout = os.Stdout
	innerCmd.Stderr = os.Stderr
	innerCmd.Stdin = os.Stdin

	// Intentionally returning xcode error unwrapped

	//nolint:wrapcheck
	return innerCmd.Run()
}
