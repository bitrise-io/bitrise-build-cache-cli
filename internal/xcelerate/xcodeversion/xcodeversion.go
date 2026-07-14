// Package xcodeversion resolves Xcode's marketing version and build number
// from `xcodebuild -version`.
package xcodeversion

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

var (
	versionRegexp     = regexp.MustCompile(`Xcode\s+(.*)`)
	buildNumberRegexp = regexp.MustCompile(`Build version\s+(.*)`)
)

// CommandFunc matches configcommon.CommandFunc so callers can share a single
// injection point.
type CommandFunc = configcommon.CommandFunc

// Resolve invokes `<xcodePath> -version` via cmdFunc and returns the parsed
// marketing version and build number. `ctx` is accepted for future use — the
// injected CommandFunc has no context-aware form today, so it's currently
// ignored.
func Resolve(_ context.Context, xcodePath string, cmdFunc CommandFunc) (string, string, error) {
	if xcodePath == "" {
		return "", "", fmt.Errorf("xcodebuild path is empty")
	}
	if cmdFunc == nil {
		return "", "", fmt.Errorf("command func is nil")
	}

	output, err := cmdFunc(xcodePath, "-version")
	if err != nil {
		return "", "", fmt.Errorf("xcodebuild -version failed: %w", err)
	}

	return parseVersionOutput(output)
}

func parseVersionOutput(output string) (string, string, error) {
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return "", "", fmt.Errorf("unexpected xcodebuild -version output: %s", output)
	}

	versionMatch := versionRegexp.FindStringSubmatch(strings.TrimSpace(lines[0]))
	if len(versionMatch) < 2 {
		return "", "", fmt.Errorf("failed to parse xcode version from: %s", lines[0])
	}

	buildNumberMatch := buildNumberRegexp.FindStringSubmatch(strings.TrimSpace(lines[1]))
	if len(buildNumberMatch) < 2 {
		return "", "", fmt.Errorf("failed to parse xcode build number from: %s", lines[1])
	}

	return versionMatch[1], buildNumberMatch[1], nil
}
