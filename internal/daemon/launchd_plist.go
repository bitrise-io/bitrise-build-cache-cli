package daemon

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"text/template"
)

// plistTemplate is the LaunchAgent plist body. KeepAlive=true restarts the
// process if it dies. RunAtLoad=true starts it immediately on bootstrap and
// at user-login. ProcessType=Background opts into launchd's lower-priority
// scheduling tier — fine for caching proxies.
//
// We embed Label inside ProgramArguments by referring to fields directly
// rather than passing a marshalled XML attribute, because Apple's plist parser
// is positional + indent-tolerant but easier to debug when the file reads
// like the ones Xcode ships.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
{{range .ProgramArguments}}		<string>{{escape .}}</string>
{{end}}	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>{{escape .StdoutPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{escape .StderrPath}}</string>
</dict>
</plist>
`

// plistData is the template input. Kept package-private to discourage callers
// from constructing plists by hand — use GeneratePlist.
type plistData struct {
	Label            string
	ProgramArguments []string
	StdoutPath       string
	StderrPath       string
}

// GeneratePlist renders a LaunchAgent plist for the given service using the
// supplied CLI executable path and Paths.
func GeneratePlist(svc Service, executable string, paths Paths) (string, error) {
	if executable == "" {
		return "", fmt.Errorf("executable path is empty")
	}

	tmpl, err := template.New("plist").Funcs(template.FuncMap{
		"escape": xmlEscapeString,
	}).Parse(plistTemplate)
	if err != nil {
		return "", fmt.Errorf("parse plist template: %w", err)
	}

	args := append([]string{executable}, svc.Args...)

	data := plistData{
		Label:            svc.Label(),
		ProgramArguments: args,
		StdoutPath:       paths.StdoutPath(svc.Name),
		StderrPath:       paths.StderrPath(svc.Name),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render plist template: %w", err)
	}

	return buf.String(), nil
}

// xmlEscapeString escapes a single string for use inside an XML text node.
// The stdlib xml.EscapeText writes to an io.Writer; we want a return value
// usable from text/template.
func xmlEscapeString(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		// xml.EscapeText only errors on writer failure, which can't happen on
		// bytes.Buffer. Fall back to the un-escaped value so a future stdlib
		// behaviour change doesn't take the whole command down.
		return s
	}

	return strings.ReplaceAll(buf.String(), "\t", "&#9;")
}
