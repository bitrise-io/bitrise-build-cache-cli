package cmd

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/pathutil"
)

func readFileIfExists(pth string) (string, bool, error) {
	fileContent := ""
	isFileExist, err := pathutil.NewPathChecker().IsPathExists(pth)
	if err != nil {
		return "", false, fmt.Errorf("check if file exists at %s, error: %w", pth, err)
	}

	if isFileExist {
		fContent, err := os.ReadFile(pth)
		if err != nil {
			return "", false, fmt.Errorf("read file at %s, error: %w", pth, err)
		}
		fileContent = string(fContent)
	}

	return fileContent, isFileExist, nil
}

func getFileSize(filePath string) (int64, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return 0, fmt.Errorf("get file info: %w", err)
	}

	return fileInfo.Size(), nil
}
