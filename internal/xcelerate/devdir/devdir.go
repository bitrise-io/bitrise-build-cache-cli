// Package devdir resolves the active Xcode developer dir. It derives from the
// configured xcodebuild path when the path shape matches
// <DeveloperDir>/usr/bin/xcodebuild, and falls back to `xcode-select -p`.
package devdir

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Resolve derives the developer dir from originalXcodebuildPath when possible so
// the result matches the CLI's Xcode; falls back to xcode-select -p.
func Resolve(ctx context.Context, cmdFunc utils.CommandFunc, originalXcodebuildPath string) (string, error) {
	if derived := FromXcodebuildPath(originalXcodebuildPath); derived != "" {
		return derived, nil
	}

	cmd := cmdFunc(ctx, "xcode-select", "-p")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("xcode-select -p: %w", err)
	}
	developerDir := strings.TrimSpace(string(out))
	if developerDir == "" {
		return "", errors.New("xcode-select -p returned an empty developer dir")
	}

	return developerDir, nil
}

// FromXcodebuildPath returns "" when path doesn't match
// <DeveloperDir>/usr/bin/xcodebuild, signalling the caller to fall back.
func FromXcodebuildPath(path string) string {
	const suffix = "/usr/bin/xcodebuild"
	if !strings.HasSuffix(path, suffix) {
		return ""
	}

	return strings.TrimSuffix(path, suffix)
}
