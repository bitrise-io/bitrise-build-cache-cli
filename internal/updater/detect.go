package updater

import (
	"os"
	"path/filepath"
	"strings"
)

type InstallMethod int

const (
	InstallUnknown InstallMethod = iota
	InstallBrew
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

//nolint:gochecknoglobals
var brewSubstrings = []string{
	"/Cellar/",
	"/opt/homebrew/",
	"/usr/local/Homebrew/",
	"/home/linuxbrew/.linuxbrew/",
	".linuxbrew/Cellar/",
}

// DetectInstallMethod falls back to $HOMEBREW_PREFIX for relocated/custom brew prefixes.
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

	if prefix := strings.TrimSpace(os.Getenv("HOMEBREW_PREFIX")); prefix != "" {
		if strings.HasPrefix(abs, filepath.Clean(prefix)+string(filepath.Separator)) {
			return InstallBrew
		}
	}

	if filepath.Base(abs) == "" || filepath.Base(abs) == "." {
		return InstallUnknown
	}

	return InstallManual
}
