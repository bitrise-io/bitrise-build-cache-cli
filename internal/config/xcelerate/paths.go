package xcelerate

import (
	"os"
	"path/filepath"
)

const (
	xceleratePath       = ".bitrise-xcelerate/"
	Xcodebuild          = "xcodebuild"
	ErrFmtDetermineHome = `could not determine home: %w`
)

func DirPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, xceleratePath)
	}

	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, xceleratePath)
	}

	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return filepath.Join(dir, xceleratePath)
		}
	}

	// last resort
	return filepath.Join(".", xceleratePath)
}

func PathFor(subpath string) string {
	return filepath.Join(DirPath(), subpath)
}
