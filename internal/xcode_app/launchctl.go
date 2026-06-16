package xcode_app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

// LaunchctlBin is the absolute path of the launchctl binary on macOS.
const LaunchctlBin = "/bin/launchctl"

// XCConfigEnvVar is the env var Xcode reads for an override xcconfig path.
const XCConfigEnvVar = "XCODE_XCCONFIG_FILE"

// LaunchctlClient wraps the `launchctl` verbs needed to drive an XCODE_XCCONFIG_FILE override.
type LaunchctlClient struct {
	// Runner executes launchctl. nil = daemon.ExecRunner (shared locale pinning + exit propagation).
	Runner daemon.CommandRunner
	// Bin overrides the launchctl binary path; empty = LaunchctlBin.
	Bin string
}

func (c LaunchctlClient) runner() daemon.CommandRunner {
	if c.Runner != nil {
		return c.Runner
	}

	return daemon.ExecRunner{}
}

func (c LaunchctlClient) bin() string {
	if c.Bin != "" {
		return c.Bin
	}

	return LaunchctlBin
}

// Setenv runs `launchctl setenv <key> <value>` so GUI-launched processes inherit it.
// Lasts only until the user logs out — pair with the LaunchAgent to survive logout.
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

// Unsetenv runs `launchctl unsetenv <key>` idempotently — launchctl returns 113 when already unset.
func (c LaunchctlClient) Unsetenv(ctx context.Context, key string) error {
	if _, _, _, err := c.runner().Run(ctx, c.bin(), "unsetenv", key); err != nil { //nolint:dogsled // matches the runner contract; we intentionally ignore stdout/stderr/exit
		return fmt.Errorf("launchctl unsetenv: %w", err)
	}

	return nil
}

// Bootstrap loads a LaunchAgent plist into the user's GUI session.
// Idempotent: pre-boots out any prior load so a stale plist is replaced.
func (c LaunchctlClient) Bootstrap(ctx context.Context, plistPath string) error {
	target := guiTarget()

	// Best-effort pre-bootout: "not loaded" is fine, but a runner-side failure means launchctl itself couldn't run.
	if _, _, _, runErr := c.runner().Run(ctx, c.bin(), "bootout", target, plistPath); runErr != nil { //nolint:dogsled // runner returns stdout/stderr/exit/err — we only care about err here
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

// Bootout removes a LaunchAgent plist from the user's GUI session.
// Idempotent: "not loaded" is treated as success.
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
