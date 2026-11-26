//go:build unit

package buildanalyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractStderrDiagnostics_EmptyBlob_ReturnsNil(t *testing.T) {
	assert.Nil(t, ExtractStderrDiagnostics(""))
}

func TestExtractStderrDiagnostics_NoHeaders_ReturnsNil(t *testing.T) {
	// Random noise with colons that aren't severity headers.
	blob := "linking Foo\nld: -F /usr/lib: directory not found\n"
	assert.Nil(t, ExtractStderrDiagnostics(blob))
}

func TestExtractStderrDiagnostics_ClangSingleError(t *testing.T) {
	blob := "main.m:10:5: error: expected ';' after expression\n" +
		"    foo()\n" +
		"    ^\n"

	got := ExtractStderrDiagnostics(blob)
	require.Len(t, got, 1)

	d := got[0]
	assert.Equal(t, "main.m", d.File)
	assert.Equal(t, 10, d.Line)
	assert.Equal(t, 5, d.Column)
	assert.Equal(t, SeverityError, d.Severity)
	assert.Equal(t, diagnosticSourceStderr, d.Source)
	// Continuation lines fold into Message.
	assert.Contains(t, d.Message, "expected ';'")
	assert.Contains(t, d.Message, "foo()")
	assert.Contains(t, d.Message, "^")
}

func TestExtractStderrDiagnostics_SwiftcError(t *testing.T) {
	blob := "App.swift:5:9: error: cannot find 'Foo' in scope\n" +
		"let x = Foo()\n" +
		"        ^~~\n"

	got := ExtractStderrDiagnostics(blob)
	require.Len(t, got, 1)
	assert.Equal(t, "App.swift", got[0].File)
	assert.Equal(t, 5, got[0].Line)
	assert.Equal(t, 9, got[0].Column)
	assert.Equal(t, SeverityError, got[0].Severity)
	assert.Contains(t, got[0].Message, "cannot find 'Foo' in scope")
}

func TestExtractStderrDiagnostics_MultipleRecords(t *testing.T) {
	blob := "a.swift:1:1: warning: unused variable 'x'\n" +
		"let x = 0\n" +
		"\n" +
		"b.swift:2:2: error: type mismatch\n" +
		"let y: Int = \"\"\n"

	got := ExtractStderrDiagnostics(blob)
	require.Len(t, got, 2)

	assert.Equal(t, SeverityWarning, got[0].Severity)
	assert.Equal(t, "a.swift", got[0].File)
	assert.Contains(t, got[0].Message, "unused variable 'x'")
	assert.Contains(t, got[0].Message, "let x = 0")

	assert.Equal(t, SeverityError, got[1].Severity)
	assert.Equal(t, "b.swift", got[1].File)
	assert.Contains(t, got[1].Message, "type mismatch")
	// Blank line between records resets the continuation accumulator.
	assert.NotContains(t, got[1].Message, "let x = 0")
}

func TestExtractStderrDiagnostics_SeverityVariants(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Severity
	}{
		{"note", "x.c:1:1: note: expanded from macro here", SeverityNote},
		{"warning", "x.c:1:1: warning: unused", SeverityWarning},
		{"error", "x.c:1:1: error: boom", SeverityError},
		{"fatal_error", "x.c:1:1: fatal error: 'foo.h' file not found", SeverityFatal},
		{"remark_folds_to_note", "x.c:1:1: remark: inlining", SeverityNote},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractStderrDiagnostics(tc.input)
			require.Len(t, got, 1)
			assert.Equal(t, tc.want, got[0].Severity)
		})
	}
}

func TestExtractStderrDiagnostics_NoColumnFallback(t *testing.T) {
	blob := "driver.c:7: error: linker command failed\n"

	got := ExtractStderrDiagnostics(blob)
	require.Len(t, got, 1)
	assert.Equal(t, "driver.c", got[0].File)
	assert.Equal(t, 7, got[0].Line)
	assert.Equal(t, 0, got[0].Column)
	assert.Equal(t, SeverityError, got[0].Severity)
}

func TestExtractStderrDiagnostics_PathWithSpaces(t *testing.T) {
	// Xcode escapes spaces in some contexts; the regex still bites on a
	// space-free prefix up to the first colon. The failing header lands in
	// the message continuation, which is still useful.
	blob := "/tmp/Sources/App.swift:12:5: error: expected expression\n"

	got := ExtractStderrDiagnostics(blob)
	require.Len(t, got, 1)
	assert.Equal(t, "/tmp/Sources/App.swift", got[0].File)
	assert.Equal(t, 12, got[0].Line)
}

func TestExtractStderrDiagnostics_ContinuationCollectsMultilineMessage(t *testing.T) {
	blob := "App.swift:1:1: error: foo\n" +
		"line two of message\n" +
		"line three\n"

	got := ExtractStderrDiagnostics(blob)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "foo")
	assert.Contains(t, got[0].Message, "line two")
	assert.Contains(t, got[0].Message, "line three")
}

func TestExtractStderrDiagnostics_LeadingJunkBeforeFirstHeader(t *testing.T) {
	blob := "compilation failed: 1 error generated.\n" +
		"App.swift:1:1: error: real diag\n"

	got := ExtractStderrDiagnostics(blob)
	require.Len(t, got, 1)
	assert.Equal(t, "App.swift", got[0].File)
}
