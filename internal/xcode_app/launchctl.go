// Package xcode_app drives Xcode.app (GUI) build-cache enable/disable via launchctl + an override xcconfig. macOS-only.
package xcode_app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/exec"
)

const LaunchctlBin = "/bin/launchctl"

const XCConfigEnvVar = "XCODE_XCCONFIG_FILE"

type LaunchctlClient struct {
	Runner daemon.CommandRunner
	Bin    string
}

func (c LaunchctlClient) runner() daemon.CommandRunner {
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

// Setenv lasts only until logout — pair with the LaunchAgent to survive logout.
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
