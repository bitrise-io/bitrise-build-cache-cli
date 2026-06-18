package xcelerate

import (
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

const (
	BinDir              = "bin"
	ErrFmtDetermineHome = `could not determine home: %w`
)

// DirPath returns the xcelerate root dir (~/.bitrise-xcelerate) using osProxy's home;
// falls back to working dir / executable dir / "." when home cannot be resolved.
func DirPath(osProxy utils.OsProxy) string {
	if home, err := osProxy.UserHomeDir(); err == nil {
		return paths.FromHome(home).XcelerateRoot()
	}

	if wd, err := osProxy.Getwd(); err == nil {
		return filepath.Join(wd, paths.XcelerateRootRelative)
	}

	if exe, err := osProxy.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return filepath.Join(dir, paths.XcelerateRootRelative)
		}
	}

	return filepath.Join(".", paths.XcelerateRootRelative)
}

func PathFor(osProxy utils.OsProxy, subpath string) string {
	return filepath.Join(DirPath(osProxy), subpath)
}
