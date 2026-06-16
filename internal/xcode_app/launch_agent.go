package xcode_app

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SetenvAgentLabel is the LaunchAgent label we install to make XCODE_XCCONFIG_FILE survive logout.
const SetenvAgentLabel = "io.bitrise.build-cache.xcode-app-setenv"

// SetenvAgentPlistRelative is the LaunchAgent plist filename relative to $HOME.
const SetenvAgentPlistRelative = "Library/LaunchAgents/" + SetenvAgentLabel + ".plist"

// SetenvAgentPlistPath returns the absolute path of our setenv LaunchAgent plist.
func SetenvAgentPlistPath(home string) string {
	return filepath.Join(home, SetenvAgentPlistRelative)
}

// setenvPlistTemplate fires the one-shot `launchctl setenv` at login.
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

// RenderSetenvAgent returns the LaunchAgent plist body that re-runs the setenv at every login.
func RenderSetenvAgent(xcconfigPath string) (string, error) {
	if strings.TrimSpace(xcconfigPath) == "" {
		return "", errors.New("xcconfig path is empty")
	}

	return fmt.Sprintf(
		setenvPlistTemplate,
		xmlEscape(SetenvAgentLabel),
		xmlEscape(LaunchctlBin),
		xmlEscape(XCConfigEnvVar),
		xmlEscape(xcconfigPath),
	), nil
}

// WriteSetenvAgent renders and writes our LaunchAgent plist, creating ~/Library/LaunchAgents if missing.
func WriteSetenvAgent(home, xcconfigPath string) (string, error) {
	body, err := RenderSetenvAgent(xcconfigPath)
	if err != nil {
		return "", err
	}

	path := SetenvAgentPlistPath(home)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // plist must be world-readable for launchctl
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	return path, nil
}

// RemoveSetenvAgent removes the LaunchAgent plist idempotently (missing file = success).
func RemoveSetenvAgent(home string) (string, error) {
	path := SetenvAgentPlistPath(home)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return path, fmt.Errorf("remove %s: %w", path, err)
	}

	return path, nil
}

// xmlEscape escapes a string for XML text, returning a string for direct format-string use.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		// xml.EscapeText only errors on writer failure; bytes.Buffer can't produce one.
		return s
	}

	return strings.ReplaceAll(buf.String(), "\t", "&#9;")
}
