package utils

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/pathutil"
)

type OsProxy struct {
	ReadFileIfExists func(pth string) (string, bool, error)
	MkdirAll         func(string, os.FileMode) error
	WriteFile        func(string, []byte, os.FileMode) error
	ListDirectories  func(string) ([]string, error)
}

func DefaultOsProxy() OsProxy {
	return OsProxy{
		ReadFileIfExists: readFileIfExists,
		MkdirAll:         os.MkdirAll,
		WriteFile:        os.WriteFile,
		ListDirectories:  listDirectories,
	}
}

func readFileIfExists(pth string) (string, bool, error) {
	if exists, err := pathutil.NewPathChecker().IsPathExists(pth); err != nil {
		return "", false, fmt.Errorf("failed to check if path (%s) exists: %w", pth, err)
	} else if !exists {
		return "", false, nil
	}

	content, err := os.ReadFile(pth)
	if err != nil {
		return "", true, fmt.Errorf("failed to read file: %s, error: %w", pth, err)
	}

	return string(content), true, nil
}

func listDirectories(pth string) ([]string, error) {
	dirs, err := os.ReadDir(pth)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", pth, err)
	}

	var dirNames []string
	for _, dir := range dirs {
		if dir.IsDir() {
			dirNames = append(dirNames, dir.Name())
		}
	}

	return dirNames, nil
}
