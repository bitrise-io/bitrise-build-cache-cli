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

// SetenvAgentLabel is the LaunchAgent label we install to make the
// XCODE_XCCONFIG_FILE setenv survive logout. Distinct from the daemon's
// xcelerate-proxy label so wholesale uninstall of one doesn't disturb the
// other.
const SetenvAgentLabel = "io.bitrise.build-cache.xcode-app-setenv"

// SetenvAgentPlistRelative is the LaunchAgent plist filename — same as
// SetenvAgentLabel with `.plist` appended.
const SetenvAgentPlistRelative = "Library/LaunchAgents/" + SetenvAgentLabel + ".plist"

// SetenvAgentPlistPath returns the absolute path of our setenv LaunchAgent
// plist under ~/Library/LaunchAgents/.
func SetenvAgentPlistPath(home string) string {
	return filepath.Join(home, SetenvAgentPlistRelative)
}

// setenvPlistTemplate is the LaunchAgent body. RunAtLoad=true fires once at
// login; KeepAlive=false (the default — we omit the key) lets the one-shot
// `launchctl setenv` call exit cleanly without a restart loop. The agent
// re-applies our XCODE_XCCONFIG_FILE override on every login, which is the
// whole point of the file.
//
// We write the launchctl call verbatim rather than chain through `sh -c …`
// to avoid quoting hazards on paths with spaces.
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

// RenderSetenvAgent returns the LaunchAgent plist body that re-runs
// `launchctl setenv XCODE_XCCONFIG_FILE <xcconfigPath>` at every login.
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

// WriteSetenvAgent renders and writes our LaunchAgent plist. mkdir the
// ~/Library/LaunchAgents directory if missing — fresh user accounts don't
// always have it.
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

// RemoveSetenvAgent removes the LaunchAgent plist. Missing file is treated
// as success so `disable` is idempotent.
func RemoveSetenvAgent(home string) (string, error) {
	path := SetenvAgentPlistPath(home)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return path, fmt.Errorf("remove %s: %w", path, err)
	}

	return path, nil
}

// xmlEscape escapes a string for use as XML text. We avoid the stdlib's
// io.Writer-based EscapeText surface so callers can drop the return value
// straight into a format string.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		// xml.EscapeText only errors on writer failure, which a
		// bytes.Buffer can't produce — but if it ever did, falling back
		// to the un-escaped value keeps us from crashing on a setenv
		// install. The plist may then be malformed; launchctl would
		// reject it loudly.
		return s
	}

	return strings.ReplaceAll(buf.String(), "\t", "&#9;")
}
