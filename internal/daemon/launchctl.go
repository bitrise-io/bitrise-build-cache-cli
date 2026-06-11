package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Runner abstracts the launchctl invocation so tests can record calls without
// touching the real launchd. The default implementation shells out to
// /bin/launchctl.
type Runner interface {
	Run(ctx context.Context, args ...string) (stdout string, stderr string, exitCode int, err error)
}

// ExecRunner is the production Runner — runs /bin/launchctl via os/exec.
type ExecRunner struct{}

// Run executes /bin/launchctl with the supplied args. Returns combined stdout,
// stderr, exit code and the *os/exec* error (nil for non-zero exits — the
// caller inspects exitCode instead).
func (ExecRunner) Run(ctx context.Context, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, "/bin/launchctl", args...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), stderr.String(), exitErr.ExitCode(), nil
		}

		return stdout.String(), stderr.String(), -1, fmt.Errorf("run launchctl: %w", err)
	}

	return stdout.String(), stderr.String(), 0, nil
}

// gui builds the launchctl service target for the current user. launchctl
// since macOS 10.10 prefers `gui/<uid>` over the deprecated `launchctl load`
// form, which is why we don't use `launchctl load -w`.
func guiTarget() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

// Bootstrap registers the plist with launchd and starts the service.
// Idempotent in spirit: if the service is already loaded we bootout first.
// Errors from bootout are deliberately swallowed (typical "service not found"
// exit 5 on first install).
func Bootstrap(ctx context.Context, runner Runner, plistPath string) error {
	// Bootout first to make this command rerunnable after a binary upgrade —
	// otherwise launchd keeps the old executable handle. Errors and non-zero
	// exits (typically exit 5 "service not loaded") are intentionally
	// ignored — that's the expected state on a first install.
	if _, _, _, runErr := runner.Run(ctx, "bootout", guiTarget(), plistPath); runErr != nil {
		_ = runErr
	}

	_, stderr, code, err := runner.Run(ctx, "bootstrap", guiTarget(), plistPath)
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}

	if code != 0 {
		return fmt.Errorf("launchctl bootstrap %s exited %d: %s", plistPath, code, strings.TrimSpace(stderr))
	}

	return nil
}

// Bootout unloads the service if registered. Returns nil if the service was
// not loaded — uninstall callers expect that to be a no-op.
func Bootout(ctx context.Context, runner Runner, plistPath string) error {
	_, stderr, code, err := runner.Run(ctx, "bootout", guiTarget(), plistPath)
	if err != nil {
		return fmt.Errorf("launchctl bootout: %w", err)
	}

	// Exit 5 (ESRCH "no such process") means the service wasn't loaded — treat
	// as success so `daemon uninstall` is idempotent.
	if code != 0 && code != 5 {
		return fmt.Errorf("launchctl bootout %s exited %d: %s", plistPath, code, strings.TrimSpace(stderr))
	}

	return nil
}
