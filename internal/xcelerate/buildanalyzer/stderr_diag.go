package buildanalyzer

import (
	"regexp"
	"strconv"
	"strings"
)

// diagnosticSourceStderr marks records produced by ExtractStderrDiagnostics so
// downstream consumers can distinguish them from `.dia`-sourced records once
// that path ships.
const diagnosticSourceStderr = "stderr"

// stderrDiagHeaderRegexp matches the canonical `path:line:col: severity: msg`
// clang / swiftc diagnostic header. Works for both tools:
//
//	main.m:10:5: error: expected ';'
//	App.swift:5:9: error: cannot find 'Foo' in scope
//	src/foo.cpp:42:1: warning: unused variable 'x'
//
// Column is permitted to be absent in a few clang modes (`file:line: error:
// ...`); the second pattern covers that. The file capture deliberately uses a
// greedy `[^:]+` scope so Windows-style drive letters (`C:\foo`) would need a
// tweak — we don't target Windows paths, and Xcode emits POSIX paths.
//
//nolint:gochecknoglobals // compiled once, read-only
var stderrDiagHeaderRegexp = regexp.MustCompile(
	`^(?P<file>[^:\s][^:]*):(?P<line>\d+):(?P<col>\d+):\s+` +
		`(?P<severity>note|warning|error|fatal error|remark):\s*(?P<msg>.*)$`,
)

// stderrDiagHeaderNoColRegexp is the no-column fallback — `path:line: sev:
// msg` — occasionally emitted by clang for driver-level errors that lack a
// source column.
//
//nolint:gochecknoglobals // compiled once, read-only
var stderrDiagHeaderNoColRegexp = regexp.MustCompile(
	`^(?P<file>[^:\s][^:]*):(?P<line>\d+):\s+` +
		`(?P<severity>note|warning|error|fatal error|remark):\s*(?P<msg>.*)$`,
)

// ExtractStderrDiagnostics scans a stderr blob line-by-line for clang / swiftc
// diagnostic headers and returns one Diagnostic per header. Subsequent lines
// that are NOT themselves a header are appended to the preceding diagnostic's
// Message as a continuation (clang prints the source line + caret under the
// header; swiftc does the same). The continuation stops at the next header or
// at a blank line.
//
// Returns nil (no allocation) when the blob holds no recognizable headers.
func ExtractStderrDiagnostics(blob string) []Diagnostic {
	if blob == "" {
		return nil
	}

	var out []Diagnostic

	var cur *Diagnostic

	for _, raw := range strings.Split(blob, "\n") {
		line := strings.TrimRight(raw, "\r")

		diag, ok := parseDiagnosticHeader(line)
		if ok {
			out = append(out, diag)
			cur = &out[len(out)-1]

			continue
		}

		if cur == nil {
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			cur = nil

			continue
		}

		if cur.Message == "" {
			cur.Message = trimmed
		} else {
			cur.Message = cur.Message + "\n" + trimmed
		}
	}

	return out
}

// parseDiagnosticHeader returns a populated Diagnostic + true when line is a
// `path:line:col: sev: msg` or `path:line: sev: msg` header. The no-column
// variant surfaces Column=0 — JSON omitempty hides the field.
func parseDiagnosticHeader(line string) (Diagnostic, bool) {
	if m := stderrDiagHeaderRegexp.FindStringSubmatch(line); m != nil {
		return diagnosticFromMatch(stderrDiagHeaderRegexp, m, true), true
	}

	if m := stderrDiagHeaderNoColRegexp.FindStringSubmatch(line); m != nil {
		return diagnosticFromMatch(stderrDiagHeaderNoColRegexp, m, false), true
	}

	return Diagnostic{}, false
}

func diagnosticFromMatch(re *regexp.Regexp, match []string, hasColumn bool) Diagnostic {
	byName := func(name string) string {
		idx := re.SubexpIndex(name)
		if idx < 0 || idx >= len(match) {
			return ""
		}

		return match[idx]
	}

	diag := Diagnostic{
		File:     byName("file"),
		Line:     atoiOrZero(byName("line")),
		Severity: normalizeSeverity(byName("severity")),
		Message:  strings.TrimSpace(byName("msg")),
		Source:   diagnosticSourceStderr,
	}
	if hasColumn {
		diag.Column = atoiOrZero(byName("col"))
	}

	return diag
}

// normalizeSeverity folds clang's two multi-word labels into the Severity
// enum: `fatal error` -> fatal, `remark` -> note. Everything else maps 1:1.
func normalizeSeverity(label string) Severity {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "note", "remark":
		return SeverityNote
	case "warning":
		return SeverityWarning
	case "error":
		return SeverityError
	case "fatal error", "fatal":
		return SeverityFatal
	default:
		return ""
	}
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}

	return n
}
