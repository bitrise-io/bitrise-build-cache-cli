package xcode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

func checksumOfFile(path string) (string, error) {
	hash := sha256.New()

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close() //nolint:errcheck

	_, err = io.Copy(hash, file)
	if err != nil {
		return "", fmt.Errorf("reading file to hash: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
