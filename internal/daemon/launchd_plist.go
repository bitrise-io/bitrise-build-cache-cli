package daemon

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"text/template"
)

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

type plistData struct {
	Label            string
	ProgramArguments []string
	StdoutPath       string
	StderrPath       string
}

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
		StdoutPath:       paths.DaemonStdoutPath(svc.Name),
		StderrPath:       paths.DaemonStderrPath(svc.Name),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render plist template: %w", err)
	}

	return buf.String(), nil
}

func xmlEscapeString(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return s
	}

	return strings.ReplaceAll(buf.String(), "\t", "&#9;")
}
