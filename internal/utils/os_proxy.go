package utils

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/pathutil"
)

//go:generate moq -rm -out ./os_proxy_mock.go . OsProxy:MockOsProxy
//nolint:inamedparam
type OsProxy interface {
	ReadFileIfExists(pth string) (string, bool, error)
	MkdirAll(string, os.FileMode) error
	WriteFile(string, []byte, os.FileMode) error
	UserHomeDir() (string, error)
	Create(string) (*os.File, error)
}

type DefaultOsProxy struct{}

func (d DefaultOsProxy) ReadFileIfExists(pth string) (string, bool, error) {
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

func (d DefaultOsProxy) MkdirAll(pth string, perm os.FileMode) error {
	return os.MkdirAll(pth, perm) //nolint:wrapcheck
}

func (d DefaultOsProxy) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm) //nolint:wrapcheck
}

func (d DefaultOsProxy) UserHomeDir() (string, error) {
	return os.UserHomeDir() //nolint:wrapcheck
}

func (d DefaultOsProxy) Create(name string) (*os.File, error) {
	return os.Create(name) //nolint:wrapcheck
}
