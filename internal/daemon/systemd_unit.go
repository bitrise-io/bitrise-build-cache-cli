package daemon

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// unitTemplate is the systemd user unit body.
//
//   - Type=simple matches the proxy + helper, which run in the foreground and
//     hold their socket / state until killed.
//   - Restart=on-failure + RestartSec=5 gives launchd-equivalent crash
//     recovery without restarting on clean shutdowns.
//   - WantedBy=default.target enables the unit for the user's login session.
//   - Logs go to journald — `journalctl --user -u <unit>` is the supported
//     way to read them, mirroring the macOS path of "tail the supervisor's
//     stdout/stderr log".
const unitTemplate = `[Unit]
Description=Bitrise Build Cache — {{.Description}}
After=default.target

[Service]
Type=simple
ExecStart={{.ExecStart}}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

type unitData struct {
	Description string
	ExecStart   string
}

// GenerateUnit renders a systemd user unit for svc using the supplied CLI
// executable path. The ExecStart line is built from the executable plus
// svc.Args, with each segment shell-escaped (systemd parses ExecStart as a
// space-separated argv with simple quoting).
func GenerateUnit(svc Service, executable string) (string, error) {
	if executable == "" {
		return "", fmt.Errorf("executable path is empty")
	}

	args := append([]string{executable}, svc.Args...)

	escaped := make([]string, 0, len(args))
	for _, a := range args {
		escaped = append(escaped, escapeForUnit(a))
	}

	tmpl, err := template.New("unit").Parse(unitTemplate)
	if err != nil {
		return "", fmt.Errorf("parse unit template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, unitData{
		Description: svc.Name,
		ExecStart:   strings.Join(escaped, " "),
	}); err != nil {
		return "", fmt.Errorf("render unit template: %w", err)
	}

	return buf.String(), nil
}

// escapeForUnit applies the minimum quoting systemd's ExecStart parser
// requires. systemd uses POSIX-shell-like quoting; bare paths and short
// alphanumeric tokens pass through unchanged. Anything containing whitespace
// or a quote gets wrapped in double quotes with internal quotes escaped.
func escapeForUnit(s string) string {
	if s == "" {
		return `""`
	}

	if !needsUnitQuoting(s) {
		return s
	}

	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func needsUnitQuoting(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '/' || r == '-' || r == '_' || r == '.' || r == '=' || r == ':':
			continue
		default:
			return true
		}
	}

	return false
}
