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
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<!-- Interactive, because every other band is throttled under contention and
	     this proxy serves xcodebuild while all cores compile. A shell child
	     inherits the shell's QoS; a launchd job does not. Under a real build on
	     a 4-core CI VM, Background cost 5276ms against 16ms per cache
	     operation; Interactive matched the shell, 3 runs of 3, and production
	     went from 106 failed builds in 22h to none.

	     Only reproducible on a small machine under a real compile:
	     e2e-daemon-cache-macos runs 4-core on purpose, where flipping this to
	     Background timed out 128 of 620 cache operations while Interactive
	     timed out none. On a 6-core machine both bands measure identically, so
	     do not re-tune this from a microbenchmark or a larger VM. -->
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>ProcessType</key>
	<string>Interactive</string>
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
