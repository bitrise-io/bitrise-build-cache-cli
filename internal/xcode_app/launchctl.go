package xcode_app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/exec"
)

// LaunchctlBin is the absolute path to macOS's launchctl.
const LaunchctlBin = "/bin/launchctl"

// LaunchctlClient wraps `launchctl setenv/getenv/unsetenv/bootstrap/bootout`.
// Runner is injectable for tests; Bin defaults to LaunchctlBin.
type LaunchctlClient struct {
	Runner exec.Runner
	Bin    string
}

func (c LaunchctlClient) runner() exec.Runner {
	if c.Runner != nil {
		return c.Runner
	}

	return exec.ExecRunner{PinLocale: true}
}

func (c LaunchctlClient) bin() string {
	if c.Bin != "" {
		return c.Bin
	}

	return LaunchctlBin
}

// Setenv lasts only until logout — pair with the LaunchAgent to survive it.
func (c LaunchctlClient) Setenv(ctx context.Context, key, value string) error {
	_, stderr, code, err := c.runner().Run(ctx, c.bin(), "setenv", key, value)
	if err != nil {
		return fmt.Errorf("launchctl setenv: %w", err)
	}

	if code != 0 {
		return fmt.Errorf("launchctl setenv %s exited %d: %s", key, code, strings.TrimSpace(stderr))
	}

	return nil
}

// Getenv returns the current value or "" when the key is unset.
func (c LaunchctlClient) Getenv(ctx context.Context, key string) (string, error) {
	stdout, stderr, code, err := c.runner().Run(ctx, c.bin(), "getenv", key)
	if err != nil {
		return "", fmt.Errorf("launchctl getenv: %w", err)
	}

	switch code {
	case 0:
		return strings.TrimRight(stdout, "\n"), nil
	case 113:
		return "", nil
	default:
		return "", fmt.Errorf("launchctl getenv %s exited %d: %s", key, code, strings.TrimSpace(stderr))
	}
}

// Unsetenv treats launchctl exit 113 ("already unset") as success.
func (c LaunchctlClient) Unsetenv(ctx context.Context, key string) error {
	_, stderr, code, err := c.runner().Run(ctx, c.bin(), "unsetenv", key)
	if err != nil {
		return fmt.Errorf("launchctl unsetenv: %w", err)
	}

	if code != 0 && code != 113 {
		return fmt.Errorf("launchctl unsetenv %s exited %d: %s", key, code, strings.TrimSpace(stderr))
	}

	return nil
}

// Bootstrap pre-boots out any prior load so a stale plist is replaced.
func (c LaunchctlClient) Bootstrap(ctx context.Context, plistPath string) error {
	target := guiTarget()

	if _, _, _, runErr := c.runner().Run(ctx, c.bin(), "bootout", target, plistPath); runErr != nil { //nolint:dogsled // runner returns stdout/stderr/exit/err — only err matters here
		return fmt.Errorf("launchctl bootout (pre-bootstrap): %w", runErr)
	}

	_, stderr, code, err := c.runner().Run(ctx, c.bin(), "bootstrap", target, plistPath)
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}

	if code != 0 {
		return fmt.Errorf("launchctl bootstrap %s exited %d: %s", plistPath, code, strings.TrimSpace(stderr))
	}

	return nil
}

// Bootout treats "not loaded" stderr as success.
func (c LaunchctlClient) Bootout(ctx context.Context, plistPath string) error {
	_, stderr, code, err := c.runner().Run(ctx, c.bin(), "bootout", guiTarget(), plistPath)
	if err != nil {
		return fmt.Errorf("launchctl bootout: %w", err)
	}

	if code != 0 && !isNotLoaded(stderr) {
		return fmt.Errorf("launchctl bootout %s exited %d: %s", plistPath, code, strings.TrimSpace(stderr))
	}

	return nil
}

func guiTarget() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func isNotLoaded(stderr string) bool {
	s := strings.ToLower(stderr)

	return strings.Contains(s, "could not find service") ||
		strings.Contains(s, "service not loaded") ||
		strings.Contains(s, "no such process")
}
