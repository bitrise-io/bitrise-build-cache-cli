// Package updater implements the `bitrise-build-cache update` subcommand —
// the manual install upgrade path. Detects how the running CLI was installed
// (brew vs `installer.sh`), then either prints the brew upgrade command or
// re-runs installer.sh against the same bindir.
//
// Best-effort by design — the user can always fall back to running
// installer.sh manually if any of this misfires.
package updater

import (
	"path/filepath"
	"strings"
)

// InstallMethod classifies how the running binary got onto disk. Used to pick
// the right upgrade flow.
type InstallMethod int

const (
	// InstallUnknown is the safe default when path inspection didn't yield a
	// confident classification. Callers print a generic guidance message and
	// don't attempt an automated upgrade.
	InstallUnknown InstallMethod = iota
	// InstallBrew means the running binary is under a Homebrew Cellar prefix.
	InstallBrew
	// InstallManual means the running binary lives outside any brew prefix —
	// typically dropped by `installer.sh -b <bindir>`.
	InstallManual
)

func (m InstallMethod) String() string {
	switch m {
	case InstallBrew:
		return "brew"
	case InstallManual:
		return "manual"
	case InstallUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// brewSubstrings are the path fragments that mark a Homebrew install. Matched
// against the resolved (symlink-followed) absolute path of the running
// binary. Covers macOS (Apple Silicon + Intel) and Linuxbrew.
//
//nolint:gochecknoglobals
var brewSubstrings = []string{
	"/Cellar/",                    // generic — present in every brew install
	"/opt/homebrew/",              // Apple Silicon prefix (symlink target dir)
	"/usr/local/Homebrew/",        // Intel macOS Homebrew internal path
	"/home/linuxbrew/.linuxbrew/", // Linuxbrew shared install
	".linuxbrew/Cellar/",          // per-user Linuxbrew
}

// DetectInstallMethod inspects the supplied executable path (typically
// os.Executable() at the call site, optionally symlink-resolved by the
// caller) and returns the best-guess install method. Pure function — no I/O —
// so it's trivially testable without setting up a real install.
func DetectInstallMethod(executable string) InstallMethod {
	if executable == "" {
		return InstallUnknown
	}

	abs := filepath.Clean(executable)

	for _, frag := range brewSubstrings {
		if strings.Contains(abs, frag) {
			return InstallBrew
		}
	}

	// Empty / unparseable basename is a hard "unknown" signal — defensive
	// against future os.Executable() return shapes.
	if filepath.Base(abs) == "" || filepath.Base(abs) == "." {
		return InstallUnknown
	}

	return InstallManual
}
