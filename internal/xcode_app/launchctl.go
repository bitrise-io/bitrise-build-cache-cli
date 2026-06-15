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
// Exported so tests can inject a stub via the LaunchctlClient.Bin field.
const LaunchctlBin = "/bin/launchctl"

// XCConfigEnvVar is the environment variable Xcode reads to discover an
// override xcconfig. We set it via `launchctl setenv` so processes launched
// from the current GUI session (including Xcode.app) inherit it.
const XCConfigEnvVar = "XCODE_XCCONFIG_FILE"

// LaunchctlClient wraps the few `launchctl` verbs we need to drive an
// XCODE_XCCONFIG_FILE override. The underlying Runner contract is shared
// with the daemon package — reusing daemon.ExecRunner gives us identical
// LC_ALL=C / LANG=C locale pinning + exit-code propagation.
type LaunchctlClient struct {
	// Runner executes the launchctl process. nil falls back to
	// daemon.ExecRunner.
	Runner daemon.CommandRunner
	// Bin overrides the launchctl binary path. nil falls back to
	// LaunchctlBin.
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

// Setenv runs `launchctl setenv <key> <value>` so future GUI-launched
// processes inherit the variable. Affects the user's current bootstrap
// session only; survives only until the user logs out. Pair with the
// LaunchAgent in launch_agent.go to make it survive logout.
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

// Unsetenv runs `launchctl unsetenv <key>`. A non-zero exit is treated as
// success because launchctl returns 113 when the variable is already
// unset — we want disable to be idempotent.
func (c LaunchctlClient) Unsetenv(ctx context.Context, key string) error {
	if _, _, _, err := c.runner().Run(ctx, c.bin(), "unsetenv", key); err != nil { //nolint:dogsled // matches the runner contract; we intentionally ignore stdout/stderr/exit
		return fmt.Errorf("launchctl unsetenv: %w", err)
	}

	return nil
}

// Bootstrap loads a LaunchAgent plist into the user's GUI session via
// `launchctl bootstrap gui/$UID <plist>`. The bootout-then-bootstrap pre-step
// (mirroring daemon.LaunchdBackend) is what makes Bootstrap idempotent — if
// the plist is already loaded, the prior version is unloaded first.
func (c LaunchctlClient) Bootstrap(ctx context.Context, plistPath string) error {
	target := guiTarget()

	// Best-effort pre-bootout: a "not loaded" exit is fine, but a runner-side
	// error means launchctl itself couldn't run, which we surface.
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

// Bootout removes a LaunchAgent plist from the user's GUI session via
// `launchctl bootout gui/$UID <plist>`. A "not loaded" exit is treated as
// success so disable is idempotent.
func (c LaunchctlClient) Bootout(ctx context.Context, plistPath string) error {
	_, stderr, code, err := c.runner().Run(ctx, c.bin(), "bootout", guiTarget(), plistPath)
	if err != nil {
		return fmt.Errorf("launchctl bootout: %w", err)
	}

	// 113 / "service not loaded" is fine — that's the disable-after-disable
	// case. Anything else is a real error.
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
