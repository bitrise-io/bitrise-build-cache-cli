package spawn

import (
	"context"
	"os"
	"runtime"
	"strconv"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/exec"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// CLI versions up to v3.6.9 registered the cache services with the OS
// supervisor. Those registrations outlive the upgrade and keep restarting a
// supervised service, which is the slow path this replaced, so every code path
// that starts a service retires one first.
const (
	legacyLabelPrefix = "io.bitrise.build-cache."
	legacyUnitPrefix  = "bitrise-build-cache-"
)

const (
	launchctlBin = "/bin/launchctl"
	systemctlBin = "/usr/bin/systemctl"
)

// launchctl bootout reports 5 when the service was not loaded.
const launchctlNotLoaded = 5

// RemoveLegacySupervision unregisters and deletes any leftover LaunchAgent or
// systemd unit for svc. Best-effort and idempotent: it reports whether it
// removed anything, never why it could not.
func RemoveLegacySupervision(ctx context.Context, p paths.Paths, svc Service) bool {
	configPath := p.UnitPath(legacyUnitPrefix + svc.Name)
	if runtime.GOOS == "darwin" {
		configPath = p.PlistPath(legacyLabelPrefix + svc.Name)
	}

	if _, err := os.Stat(configPath); err != nil {
		return false
	}

	runner := exec.ExecRunner{PinLocale: true}

	switch runtime.GOOS {
	case "darwin":
		target := "gui/" + strconv.Itoa(os.Getuid())
		if _, _, code, err := runner.Run(ctx, launchctlBin, "bootout", target, configPath); err != nil ||
			(code != 0 && code != launchctlNotLoaded) {
			return false
		}
	case "linux":
		unit := legacyUnitPrefix + svc.Name + ".service"
		//nolint:dogsled // best-effort: a unit that is already gone is the outcome we want
		_, _, _, _ = runner.Run(ctx, systemctlBin, "--user", "disable", "--now", unit)
	}

	if err := os.Remove(configPath); err != nil {
		return false
	}

	if runtime.GOOS == "linux" {
		//nolint:dogsled // best-effort
		_, _, _, _ = runner.Run(ctx, systemctlBin, "--user", "daemon-reload")
	}

	return true
}
