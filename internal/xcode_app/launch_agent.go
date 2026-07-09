package xcode_app

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Verbatim ProgramArguments (no `sh -c …`) avoid quoting hazards on paths with spaces.
const setenvPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>setenv</string>
		<string>%s</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`

func RenderSetenvAgent(xcconfigPath string) (string, error) {
	if strings.TrimSpace(xcconfigPath) == "" {
		return "", errors.New("xcconfig path is empty")
	}

	return fmt.Sprintf(
		setenvPlistTemplate,
		xmlEscape(paths.XcodeAppSetenvAgentLabel),
		xmlEscape(LaunchctlBin),
		xmlEscape(XCConfigEnvVar),
		xmlEscape(xcconfigPath),
	), nil
}

func WriteSetenvAgent(osProxy utils.OsProxy, home, xcconfigPath string) (string, error) {
	body, err := RenderSetenvAgent(xcconfigPath)
	if err != nil {
		return "", err
	}

	path := paths.FromHome(home).XcodeAppSetenvAgentPlistFile()
	dir := filepath.Dir(path)

	if err := osProxy.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	if err := osProxy.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // plist must be world-readable for launchctl
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	return path, nil
}

func RemoveSetenvAgent(osProxy utils.OsProxy, home string) (string, error) {
	path := paths.FromHome(home).XcodeAppSetenvAgentPlistFile()
	if err := osProxy.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return path, fmt.Errorf("remove %s: %w", path, err)
	}

	return path, nil
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return s
	}

	return strings.ReplaceAll(buf.String(), "\t", "&#9;")
}
